package handlers

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tellr/actions"
	"tellr/models"
)

const (
	maxImportBytes = 512 << 10
	maxImportRows  = 1000
)

type importProblem struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

var importDateLayouts = []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006", "2 Jan 2006", "02 Jan 2006", "Jan 2, 2006", "2006/01/02"}

func parseImportDate(s string, today time.Time) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return today.Format(models.DateLayout), nil
	}
	for _, l := range importDateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format(models.DateLayout), nil
		}
	}
	return "", fmt.Errorf("date %q is not recognised", clipText(s, 20))
}

func parseImportAmount(s string) (float64, error) {
	s = strings.TrimSpace(s)
	for _, junk := range []string{"₹", "Rs.", "rs.", "Rs", "rs", "INR", "inr", ",", " "} {
		s = strings.ReplaceAll(s, junk, "")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a number", clipText(s, 20))
	}
	return v, nil
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

type importColumns struct {
	date, category, amount, note int
}

func detectColumns(header []string) (importColumns, bool) {
	cols := importColumns{-1, -1, -1, -1}
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))) {
		case "date", "spent_on", "spent on", "day":
			cols.date = i
		case "category", "cat", "type":
			cols.category = i
		case "amount", "amt", "price", "cost", "rupees":
			cols.amount = i
		case "note", "notes", "description", "desc", "item", "memo", "details":
			cols.note = i
		}
	}
	return cols, cols.amount >= 0
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "That file is too big. Keep it under 500 KB.")
		return
	}
	cr := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil || len(records) == 0 {
		writeError(w, http.StatusBadRequest, "I couldn't read that file as a CSV.")
		return
	}
	cols, hasHeader := detectColumns(records[0])
	first := 0
	if hasHeader {
		first = 1
	} else {
		cols = importColumns{date: 0, category: 1, amount: 2, note: 3}
	}
	if len(records)-first > maxImportRows {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("That file has more than %d rows. Split it and import in parts.", maxImportRows))
		return
	}
	now := s.nowFor(r, "")
	var good []models.ExpenseInput
	var problems []importProblem
	skipped := 0
	custom := map[string]bool{}
	for _, n := range actions.CustomBudgetNames(r.Context(), s.DB) {
		custom[n] = true
	}
	for i := first; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 || (len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			continue
		}
		fail := func(reason string) {
			skipped++
			if len(problems) < 5 {
				problems = append(problems, importProblem{Row: i + 1, Reason: reason})
			}
		}
		date, err := parseImportDate(cell(row, cols.date), now)
		if err != nil {
			fail(err.Error())
			continue
		}
		amount, err := parseImportAmount(cell(row, cols.amount))
		if err != nil {
			fail(err.Error())
			continue
		}
		raw := models.NormalizeName(cell(row, cols.category))
		in := models.ExpenseInput{Amount: amount, Category: raw, Note: cell(row, cols.note), SpentOn: date}
		if err := in.Validate(now); err != nil {
			fail(err.Error())
			continue
		}
		if in.Category == "other" && custom[raw] {
			in.Category = raw
		}
		good = append(good, in)
	}
	if len(good) == 0 {
		msg := "No valid rows found."
		if len(problems) > 0 {
			msg += fmt.Sprintf(" Row %d: %s.", problems[0].Row, problems[0].Reason)
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	lo, hi := good[0].SpentOn, good[0].SpentOn
	for _, g := range good {
		if g.SpentOn < lo {
			lo = g.SpentOn
		}
		if g.SpentOn > hi {
			hi = g.SpentOn
		}
	}
	imported, dupes := 0, 0
	err = s.DB.Tx(r.Context(), func(repo actions.Repo) error {
		existing, err := repo.ListExpenses(r.Context(), lo, hi, 5000)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		key := func(date string, amount float64, cat, note string) string {
			return fmt.Sprintf("%s|%.2f|%s|%s", date, amount, cat, strings.ToLower(note))
		}
		for _, e := range existing {
			seen[key(e.SpentOn, e.Amount, e.Category, e.Note)] = true
		}
		for _, g := range good {
			k := key(g.SpentOn, g.Amount, g.Category, g.Note)
			if seen[k] {
				dupes++
				continue
			}
			if _, err := repo.AddExpense(r.Context(), g, now); err != nil {
				return err
			}
			seen[k] = true
			imported++
		}
		return nil
	})
	if err != nil {
		s.serverError(w, "import expenses", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported":   imported,
		"duplicates": dupes,
		"skipped":    skipped,
		"problems":   problems,
	})
}
