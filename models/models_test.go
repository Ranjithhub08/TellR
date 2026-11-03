package models

import (
	"math"
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func TestExpenseValidateDefaults(t *testing.T) {
	in := ExpenseInput{Amount: 250, Category: "  FOOD ", Note: "  lunch  "}
	if err := in.Validate(today); err != nil {
		t.Fatal(err)
	}
	if in.Category != "food" || in.Note != "lunch" || in.SpentOn != "2026-11-03" {
		t.Fatalf("got %+v", in)
	}
}

func TestExpenseValidateRoundsToPaise(t *testing.T) {
	in := ExpenseInput{Amount: 10.006}
	if err := in.Validate(today); err != nil {
		t.Fatal(err)
	}
	if in.Paise() != 1001 || in.Amount != 10.01 {
		t.Fatalf("got %+v paise=%d", in, in.Paise())
	}
}

func TestExpenseValidateRejects(t *testing.T) {
	long := strings.Repeat("a", MaxTextLength+1)
	cases := map[string]ExpenseInput{
		"zero":       {Amount: 0},
		"negative":   {Amount: -1},
		"nan":        {Amount: math.NaN()},
		"inf":        {Amount: math.Inf(1)},
		"too big":    {Amount: MaxAmount + 1},
		"too small":  {Amount: 0.004},
		"long note":  {Amount: 5, Note: long},
		"future":     {Amount: 5, SpentOn: "2026-11-04"},
		"bad format": {Amount: 5, SpentOn: "03/11/2026"},
		"fake date":  {Amount: 5, SpentOn: "2026-02-30"},
		"unpadded":   {Amount: 5, SpentOn: "2026-1-5"},
		"too old":    {Amount: 5, SpentOn: "1999-12-31"},
		"injection":  {Amount: 5, SpentOn: "2026-11-03'; DROP TABLE expenses;--"},
	}
	for name, in := range cases {
		in := in
		if err := in.Validate(today); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestUnknownCategoryFallsBack(t *testing.T) {
	in := ExpenseInput{Amount: 5, Category: "gadgets"}
	if err := in.Validate(today); err != nil || in.Category != "other" {
		t.Fatalf("got %+v err=%v", in, err)
	}
}

func TestReminderValidate(t *testing.T) {
	in := ReminderInput{Title: "  call mom ", DueDate: "2026-11-03", DueTime: "7:05"}
	if err := in.Validate(today, false); err != nil {
		t.Fatal(err)
	}
	if in.Title != "call mom" || in.DueTime != "07:05" {
		t.Fatalf("got %+v", in)
	}
}

func TestReminderValidateRejects(t *testing.T) {
	cases := map[string]ReminderInput{
		"no title":   {DueDate: "2026-11-04"},
		"no date":    {Title: "x"},
		"past":       {Title: "x", DueDate: "2026-11-02"},
		"fake date":  {Title: "x", DueDate: "2026-13-01"},
		"too far":    {Title: "x", DueDate: "2032-01-01"},
		"bad time":   {Title: "x", DueDate: "2026-11-04", DueTime: "25:00"},
		"long title": {Title: strings.Repeat("a", MaxTextLength+1), DueDate: "2026-11-04"},
	}
	for name, in := range cases {
		in := in
		if err := in.Validate(today, false); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestReminderAllowPast(t *testing.T) {
	in := ReminderInput{Title: "x", DueDate: "2026-11-01"}
	if err := in.Validate(today, true); err != nil {
		t.Fatal(err)
	}
}

func TestPeriodRange(t *testing.T) {
	cases := []struct {
		period, from, to string
		fail             bool
	}{
		{"", "2026-11-03", "2026-11-03", false},
		{"today", "2026-11-03", "2026-11-03", false},
		{"week", "2026-10-28", "2026-11-03", false},
		{"month", "2026-11-01", "2026-11-03", false},
		{"year", "", "", true},
	}
	for _, c := range cases {
		from, to, err := PeriodRange(c.period, today)
		if (err != nil) != c.fail || from != c.from || to != c.to {
			t.Errorf("%q: got %s %s %v", c.period, from, to, err)
		}
	}
}

func TestLoadLocation(t *testing.T) {
	if loc, err := LoadLocation(""); err != nil || loc == nil {
		t.Fatalf("default location: %v", err)
	}
	if _, err := LoadLocation("Not/AZone"); err == nil {
		t.Fatal("bad zone accepted")
	}
}

func TestNormaliseRepeat(t *testing.T) {
	for in, want := range map[string]string{"": "", "None": "", "once": "", " Daily ": "daily", "weekly": "weekly", "MONTHLY": "monthly"} {
		got, err := NormaliseRepeat(in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q, %v", in, got, err)
		}
	}
	if _, err := NormaliseRepeat("yearly"); err == nil {
		t.Fatal("yearly must be rejected")
	}
}

func TestNextOccurrence(t *testing.T) {
	cases := []struct{ date, repeat, today, want string }{
		{"2026-11-03", "daily", "2026-11-03", "2026-11-04"},
		{"2026-11-03", "weekly", "2026-11-03", "2026-11-10"},
		{"2026-11-03", "monthly", "2026-11-03", "2026-12-03"},
		{"2026-01-31", "monthly", "2026-01-31", "2026-02-28"},
		{"2026-10-01", "daily", "2026-11-03", "2026-11-04"},
		{"2026-10-01", "weekly", "2026-11-03", "2026-11-05"},
		{"2026-11-10", "daily", "2026-11-03", "2026-11-11"},
		{"2026-11-03", "", "2026-11-03", "2026-11-03"},
		{"bad", "daily", "2026-11-03", "bad"},
	}
	for _, c := range cases {
		if got := NextOccurrence(c.date, c.repeat, c.today); got != c.want {
			t.Fatalf("%+v got %s", c, got)
		}
	}
}

func TestBudgetInputValidate(t *testing.T) {
	ok := BudgetInput{Category: " Food ", Limit: 1234.567}
	if err := ok.Validate(); err != nil || ok.Category != "food" || ok.Limit != 1234.57 {
		t.Fatalf("%+v %v", ok, err)
	}
	all := BudgetInput{Category: "Overall", Limit: 10}
	if err := all.Validate(); err != nil || all.Category != "all" {
		t.Fatalf("%+v %v", all, err)
	}
	for _, in := range []BudgetInput{{Category: "pets", Limit: 5}, {Category: "food", Limit: 0}, {Category: "food", Limit: -1}, {Category: "food", Limit: MaxAmount + 1}} {
		if err := in.Validate(); err == nil {
			t.Fatalf("%+v should fail", in)
		}
	}
}

func TestPreviousRange(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, loc)
	for period, want := range map[string][2]string{
		"today": {"2026-11-02", "2026-11-02"},
		"week":  {"2026-10-21", "2026-10-27"},
		"month": {"2026-10-01", "2026-10-03"},
	} {
		f, to, ok := PreviousRange(period, now)
		if !ok || f != want[0] || to != want[1] {
			t.Fatalf("%s: %s %s %v", period, f, to, ok)
		}
	}
	f, to, _ := PreviousRange("month", time.Date(2026, 3, 31, 9, 0, 0, 0, loc))
	if f != "2026-02-01" || to != "2026-02-28" {
		t.Fatalf("clamp: %s %s", f, to)
	}
	if _, _, ok := PreviousRange("year", now); ok {
		t.Fatal("year is not a period")
	}
}

func TestReminderRepeatValidation(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	in := ReminderInput{Title: "x", DueDate: "2026-11-04", Repeat: " Weekly "}
	if err := in.Validate(now, false); err != nil || in.Repeat != "weekly" {
		t.Fatalf("%+v %v", in, err)
	}
	bad := ReminderInput{Title: "x", DueDate: "2026-11-04", Repeat: "hourly"}
	if err := bad.Validate(now, false); err == nil {
		t.Fatal("hourly must fail")
	}
}
