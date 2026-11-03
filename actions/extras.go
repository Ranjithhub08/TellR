package actions

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"tellr/models"
)

func BudgetStatuses(ctx context.Context, r Repo, now time.Time) ([]models.BudgetStatus, error) {
	budgets, err := r.ListBudgets(ctx)
	if err != nil {
		return nil, err
	}
	out := []models.BudgetStatus{}
	if len(budgets) == 0 {
		return out, nil
	}
	from, to, _ := models.PeriodRange("month", now)
	s, err := r.Summary(ctx, "month", from, to, now.Format(models.DateLayout))
	if err != nil {
		return nil, err
	}
	spent := map[string]float64{}
	for _, c := range s.ByCategory {
		spent[c.Category] = c.Total
	}
	for _, b := range budgets {
		sp := spent[b.Category]
		if b.Category == models.AllBudget {
			sp = s.Total
		}
		out = append(out, models.BudgetStatus{
			Category: b.Category,
			Limit:    b.Limit,
			Spent:    sp,
			Percent:  int(math.Floor(sp / b.Limit * 100)),
		})
	}
	return out, nil
}

func LoadSummary(ctx context.Context, r Repo, period string, now time.Time) (models.Summary, error) {
	from, to, err := models.PeriodRange(period, now)
	if err != nil {
		return models.Summary{}, err
	}
	s, err := r.Summary(ctx, period, from, to, now.Format(models.DateLayout))
	if err != nil {
		return models.Summary{}, err
	}
	pf, pt, ok := models.PreviousRange(period, now)
	if !ok {
		return s, nil
	}
	p, err := r.Summary(ctx, period, pf, pt, now.Format(models.DateLayout))
	if err != nil {
		return models.Summary{}, err
	}
	if p.Total > 0 {
		s.Previous = p.Total
		s.HasPrevious = true
	}
	return s, nil
}

func CompleteReminder(ctx context.Context, r Repo, m models.Reminder, today string) (models.Reminder, bool, error) {
	if m.Repeat == "" {
		updated, err := r.SetReminderDone(ctx, m.ID, true)
		return updated, false, err
	}
	updated, err := r.UpdateReminder(ctx, m.ID, models.ReminderInput{
		Title:   m.Title,
		DueDate: models.NextOccurrence(m.DueDate, m.Repeat, today),
		DueTime: m.DueTime,
		Repeat:  m.Repeat,
	})
	return updated, true, err
}

func validDate(s string) bool {
	t, err := time.Parse(models.DateLayout, s)
	return err == nil && t.Format(models.DateLayout) == s
}

func comparison(s models.Summary, period string) string {
	if !s.HasPrevious || s.Count == 0 || s.Previous <= 0 {
		return ""
	}
	diff := int(math.Round((s.Total - s.Previous) / s.Previous * 100))
	var base string
	switch period {
	case "week":
		base = "the 7 days before"
	case "month":
		base = "the same stretch last month"
	case "week2", "week3", "week4":
		base = "the week before"
	default:
		base = "yesterday"
	}
	switch {
	case diff == 0:
		return fmt.Sprintf(" That is about the same as %s.", base)
	case diff > 0:
		return fmt.Sprintf(" That is %d percent more than %s.", diff, base)
	}
	return fmt.Sprintf(" That is %d percent less than %s.", -diff, base)
}

func rangePhrase(period, from, to, today string) string {
	if period != "" {
		return expensePeriod(period)
	}
	if from != "" || to != "" {
		switch {
		case from != "" && to != "" && from == to:
			return speakDate(from, today)
		case from != "" && to != "":
			return "from " + shortDate(from) + " to " + shortDate(to)
		case from != "":
			return "since " + shortDate(from)
		}
		return "up to " + shortDate(to)
	}
	return "so far"
}

func shortDate(s string) string {
	d, err := time.Parse(models.DateLayout, s)
	if err != nil {
		return s
	}
	return d.Format("2 January")
}

func NormalizeCat(s string) string {
	return models.NormalizeName(s)
}

func CustomBudgetNames(ctx context.Context, r Repo) []string {
	list, err := r.ListBudgets(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, b := range list {
		if b.Category != models.AllBudget && !models.ValidCategory(b.Category) {
			out = append(out, b.Category)
		}
	}
	return out
}

func KeepCustom(ctx context.Context, r Repo, raw, current string) string {
	raw = models.NormalizeName(raw)
	if current != "other" || raw == "" || raw == "other" || models.ValidCategory(raw) {
		return current
	}
	for _, n := range CustomBudgetNames(ctx, r) {
		if n == raw {
			return raw
		}
	}
	return current
}

func categoryLabel(c string) string {
	if c == models.AllBudget {
		return "overall"
	}
	return c
}

func insight(ctx context.Context, r Repo, e models.Expense, today string) string {
	items, err := r.ListExpenses(ctx, e.SpentOn, e.SpentOn, 500)
	if err != nil {
		return ""
	}
	var n int
	var paise int64
	for _, it := range items {
		if it.Category == e.Category {
			n++
			paise += int64(math.Round(it.Amount * 100))
		}
	}
	if n < 2 {
		return ""
	}
	return fmt.Sprintf(" That makes %d %s expenses %s, %s so far.", n, e.Category, speakDate(e.SpentOn, today), money(float64(paise)/100))
}

func budgetWarning(ctx context.Context, r Repo, e models.Expense, now time.Time) string {
	from, to, _ := models.PeriodRange("month", now)
	if e.SpentOn < from || e.SpentOn > to {
		return ""
	}
	statuses, err := BudgetStatuses(ctx, r, now)
	if err != nil {
		return ""
	}
	var warns []string
	for _, st := range statuses {
		if st.Category != e.Category && st.Category != models.AllBudget {
			continue
		}
		before := int(math.Floor((st.Spent - e.Amount) / st.Limit * 100))
		name := categoryLabel(st.Category)
		switch {
		case st.Percent >= 100:
			warns = append(warns, fmt.Sprintf("You are over your %s budget: %s spent of %s.", name, money(st.Spent), money(st.Limit)))
		case st.Percent >= 80 && before < 80:
			warns = append(warns, fmt.Sprintf("Heads up, you have used %d percent of your %s budget.", st.Percent, name))
		}
	}
	if len(warns) == 0 {
		return ""
	}
	return " " + strings.Join(warns, " ")
}
