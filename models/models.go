package models

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxAmount       = 10000000.0
	MaxTextLength   = 200
	DateLayout      = "2006-01-02"
	TimeLayout      = "15:04"
	DefaultTimezone = "Asia/Kolkata"
	earliestDate    = "2000-01-01"
)

var ErrNotFound = errors.New("not found")

var Categories = []string{
	"food", "groceries", "transport", "travel", "shopping",
	"bills", "health", "entertainment", "education", "other",
}

func validCategory(c string) bool {
	for _, v := range Categories {
		if v == c {
			return true
		}
	}
	return false
}

func ValidCategory(c string) bool {
	return validCategory(c)
}

type Expense struct {
	ID        int64     `json:"id"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	Note      string    `json:"note"`
	SpentOn   string    `json:"spent_on"`
	CreatedAt time.Time `json:"created_at"`
}

type ExpenseInput struct {
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	Note     string  `json:"note"`
	SpentOn  string  `json:"spent_on"`
}

func (in ExpenseInput) Paise() int64 {
	return int64(math.Round(in.Amount * 100))
}

func (in *ExpenseInput) Validate(today time.Time) error {
	if math.IsNaN(in.Amount) || math.IsInf(in.Amount, 0) || in.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	if in.Amount > MaxAmount {
		return fmt.Errorf("amount must not be more than %.0f", MaxAmount)
	}
	if in.Paise() < 1 {
		return errors.New("amount must be at least 0.01")
	}
	in.Amount = float64(in.Paise()) / 100

	in.Category = strings.ToLower(strings.TrimSpace(in.Category))
	if !validCategory(in.Category) {
		in.Category = "other"
	}

	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > MaxTextLength {
		return fmt.Errorf("note must be %d characters or fewer", MaxTextLength)
	}

	todayStr := today.Format(DateLayout)
	in.SpentOn = strings.TrimSpace(in.SpentOn)
	if in.SpentOn == "" {
		in.SpentOn = todayStr
	}
	if err := checkDate(in.SpentOn); err != nil {
		return errors.New("spent_on must be a real date in YYYY-MM-DD format")
	}
	if in.SpentOn > todayStr {
		return errors.New("spent_on cannot be in the future")
	}
	if in.SpentOn < earliestDate {
		return errors.New("spent_on is too far in the past")
	}
	return nil
}

type Reminder struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	DueDate   string    `json:"due_date"`
	DueTime   string    `json:"due_time"`
	Repeat    string    `json:"repeat"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

type ReminderInput struct {
	Title   string `json:"title"`
	DueDate string `json:"due_date"`
	DueTime string `json:"due_time"`
	Repeat  string `json:"repeat"`
	Done    bool   `json:"done"`
}

func (in *ReminderInput) Validate(today time.Time, allowPast bool) error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return errors.New("title is required")
	}
	if utf8.RuneCountInString(in.Title) > MaxTextLength {
		return fmt.Errorf("title must be %d characters or fewer", MaxTextLength)
	}

	in.DueDate = strings.TrimSpace(in.DueDate)
	if in.DueDate == "" {
		return errors.New("due_date is required")
	}
	if err := checkDate(in.DueDate); err != nil {
		return errors.New("due_date must be a real date in YYYY-MM-DD format")
	}
	todayStr := today.Format(DateLayout)
	if !allowPast && in.DueDate < todayStr {
		return errors.New("due_date cannot be in the past")
	}
	if in.DueDate > today.AddDate(5, 0, 0).Format(DateLayout) {
		return errors.New("due_date is too far in the future")
	}

	rep, err := NormaliseRepeat(in.Repeat)
	if err != nil {
		return err
	}
	in.Repeat = rep

	in.DueTime = strings.TrimSpace(in.DueTime)
	if in.DueTime != "" {
		t, err := time.Parse(TimeLayout, in.DueTime)
		if err != nil {
			return errors.New("due_time must be in HH:MM 24-hour format")
		}
		in.DueTime = t.Format(TimeLayout)
	}
	return nil
}

func checkDate(s string) error {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return err
	}
	if t.Format(DateLayout) != s {
		return errors.New("not normalised")
	}
	return nil
}

type CategoryTotal struct {
	Category string  `json:"category"`
	Total    float64 `json:"total"`
	Count    int     `json:"count"`
}

type Summary struct {
	Period           string          `json:"period"`
	From             string          `json:"from"`
	To               string          `json:"to"`
	Total            float64         `json:"total"`
	Count            int             `json:"count"`
	ByCategory       []CategoryTotal `json:"by_category"`
	OpenReminders    int             `json:"open_reminders"`
	OverdueReminders int             `json:"overdue_reminders"`
	Previous         float64         `json:"previous"`
	HasPrevious      bool            `json:"has_previous"`
}

func LoadLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", name)
	}
	return loc, nil
}

func PeriodRange(period string, now time.Time) (string, string, error) {
	to := now.Format(DateLayout)
	switch period {
	case "", "today":
		return to, to, nil
	case "week":
		return now.AddDate(0, 0, -6).Format(DateLayout), to, nil
	case "month":
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return first.Format(DateLayout), to, nil
	}
	return "", "", errors.New("period must be one of: today, week, month")
}

var Repeats = []string{"daily", "weekly", "monthly"}

func NormaliseRepeat(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "none", "once", "no":
		return "", nil
	case "daily", "weekly", "monthly":
		return v, nil
	}
	return "", errors.New("repeat must be one of: daily, weekly, monthly")
}

func NextOccurrence(date, repeat, today string) string {
	d, err := time.Parse(DateLayout, date)
	if err != nil {
		return date
	}
	t, err := time.Parse(DateLayout, today)
	if err != nil {
		return date
	}
	step := func(x time.Time) time.Time {
		switch repeat {
		case "daily":
			return x.AddDate(0, 0, 1)
		case "weekly":
			return x.AddDate(0, 0, 7)
		case "monthly":
			y, m, day := x.Date()
			nm := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
			last := time.Date(nm.Year(), nm.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
			if day > last {
				day = last
			}
			return time.Date(nm.Year(), nm.Month(), day, 0, 0, 0, 0, time.UTC)
		}
		return x
	}
	if repeat != "daily" && repeat != "weekly" && repeat != "monthly" {
		return date
	}
	next := step(d)
	for i := 0; i < 4000 && !next.After(t); i++ {
		next = step(next)
	}
	return next.Format(DateLayout)
}

const AllBudget = "all"

type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

type BudgetStatus struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
	Spent    float64 `json:"spent"`
	Percent  int     `json:"percent"`
}

type BudgetInput struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

func (in *BudgetInput) Validate() error {
	in.Category = strings.ToLower(strings.TrimSpace(in.Category))
	if in.Category == "" || in.Category == "overall" || in.Category == "total" || in.Category == "everything" {
		in.Category = AllBudget
	}
	if in.Category != AllBudget && !validCategory(in.Category) {
		return errors.New("budget category must be one of the expense categories, or all")
	}
	if math.IsNaN(in.Limit) || math.IsInf(in.Limit, 0) || in.Limit <= 0 {
		return errors.New("budget must be greater than zero")
	}
	if in.Limit > MaxAmount {
		return fmt.Errorf("budget must not be more than %.0f", MaxAmount)
	}
	in.Limit = math.Round(in.Limit*100) / 100
	return nil
}

func PreviousRange(period string, now time.Time) (string, string, bool) {
	switch period {
	case "", "today":
		y := now.AddDate(0, 0, -1).Format(DateLayout)
		return y, y, true
	case "week":
		return now.AddDate(0, 0, -13).Format(DateLayout), now.AddDate(0, 0, -7).Format(DateLayout), true
	case "month":
		firstThis := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		firstPrev := firstThis.AddDate(0, -1, 0)
		lastPrev := firstThis.AddDate(0, 0, -1)
		to := time.Date(firstPrev.Year(), firstPrev.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if to.After(lastPrev) {
			to = lastPrev
		}
		return firstPrev.Format(DateLayout), to.Format(DateLayout), true
	}
	return "", "", false
}
