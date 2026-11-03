package actions

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"tellr/internal/memrepo"
	"tellr/models"
)

var ist = time.FixedZone("IST", 5*3600+1800)

func fixedNow() time.Time {
	return time.Date(2026, 11, 3, 15, 0, 0, 0, ist)
}

func newExec() (*memrepo.Repo, Executor) {
	m := memrepo.New()
	tx := func(ctx context.Context, fn func(Repo) error) error {
		return m.Tx(ctx, func(v memrepo.View) error { return fn(v) })
	}
	return m, Executor{Tx: tx}
}

func run(t *testing.T, x Executor, acts ...Action) (Outcome, error) {
	t.Helper()
	return x.Run(context.Background(), Plan{Actions: acts}, fixedNow())
}

func expenses(t *testing.T, m *memrepo.Repo) []models.Expense {
	t.Helper()
	list, err := m.ListExpenses(context.Background(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestAddExpenseToday(t *testing.T) {
	m, x := newExec()
	out, err := run(t, x, Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Changed {
		t.Fatal("changed flag not set")
	}
	if out.Reply != "Added 250 rupees for lunch under food." {
		t.Fatalf("reply: %q", out.Reply)
	}
	list := expenses(t, m)
	if len(list) != 1 || list[0].SpentOn != "2026-11-03" || list[0].Amount != 250 {
		t.Fatalf("stored: %+v", list)
	}
}

func TestAddExpenseYesterday(t *testing.T) {
	m, x := newExec()
	out, err := run(t, x, Action{Type: TypeAddExpense, Amount: 99.5, Category: "transport", Note: "cab", Date: "yesterday"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Reply, "99.50 rupees") || !strings.HasSuffix(out.Reply, "yesterday.") {
		t.Fatalf("reply: %q", out.Reply)
	}
	if got := expenses(t, m)[0].SpentOn; got != "2026-11-02" {
		t.Fatalf("date %s", got)
	}
}

func TestFutureExpenseAllowedWithinAYear(t *testing.T) {
	m, x := newExec()
	if _, err := run(t, x, Action{Type: TypeAddExpense, Amount: 2000, Category: "other", Note: "medical", Date: "2026-12-01"}); err != nil {
		t.Fatal(err)
	}
	if got := expenses(t, m); len(got) != 1 || got[0].SpentOn != "2026-12-01" {
		t.Fatalf("got %+v", got)
	}
}

func TestFarFutureExpenseRejected(t *testing.T) {
	m, x := newExec()
	_, err := run(t, x, Action{Type: TypeAddExpense, Amount: 100, Date: "2030-01-01"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("want UserError, got %v", err)
	}
	if len(expenses(t, m)) != 0 {
		t.Fatal("far future expense was stored")
	}
}

func TestBadAmountsRejected(t *testing.T) {
	for _, amt := range []float64{0, -5, math.NaN(), math.Inf(1), 99999999999} {
		m, x := newExec()
		_, err := run(t, x, Action{Type: TypeAddExpense, Amount: Num(amt), Category: "food"})
		var ue *UserError
		if !errors.As(err, &ue) {
			t.Fatalf("amount %v: want UserError, got %v", amt, err)
		}
		if len(expenses(t, m)) != 0 {
			t.Fatalf("amount %v stored", amt)
		}
	}
}

func TestUnknownCategoryBecomesOther(t *testing.T) {
	m, x := newExec()
	if _, err := run(t, x, Action{Type: TypeAddExpense, Amount: 10, Category: "spaceships"}); err != nil {
		t.Fatal(err)
	}
	if expenses(t, m)[0].Category != "other" {
		t.Fatal("category not normalised")
	}
}

func TestMultiActionIsAtomic(t *testing.T) {
	m, x := newExec()
	_, err := run(t, x,
		Action{Type: TypeAddExpense, Amount: 250, Category: "food"},
		Action{Type: TypeAddExpense, Amount: -1, Category: "food"},
	)
	if err == nil {
		t.Fatal("expected failure")
	}
	if len(expenses(t, m)) != 0 {
		t.Fatal("first action leaked despite second failing")
	}
}

func TestMultiActionAllApplied(t *testing.T) {
	m, x := newExec()
	out, err := run(t, x,
		Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"},
		Action{Type: TypeAddReminder, Title: "call mom", Date: "today", Time: "6 PM"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Done) != 2 {
		t.Fatalf("done: %v", out.Done)
	}
	rem, _ := m.ListReminders(context.Background(), true, 10)
	if len(rem) != 1 || rem[0].DueTime != "18:00" {
		t.Fatalf("reminder: %+v", rem)
	}
	if !strings.Contains(out.Reply, "Reminder set: call mom, today at 6:00 PM.") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	m, x := newExec()
	run(t, x, Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"})
	out, err := run(t, x, Action{Type: TypeDeleteExpense, ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Pending) != 1 || out.Changed {
		t.Fatalf("pending=%v changed=%v", out.Pending, out.Changed)
	}
	if len(expenses(t, m)) != 1 {
		t.Fatal("expense deleted before confirmation")
	}
	if !strings.Contains(out.Reply, "Say yes or no") {
		t.Fatalf("reply: %q", out.Reply)
	}
	reply, err := ApplyDeletes(context.Background(), x.Tx, out.Pending)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Deleted 250 rupees for lunch." {
		t.Fatalf("reply: %q", reply)
	}
	if len(expenses(t, m)) != 0 {
		t.Fatal("expense still there after confirm")
	}
	again, err := ApplyDeletes(context.Background(), x.Tx, out.Pending)
	if err != nil || !strings.Contains(again, "already gone") {
		t.Fatalf("second apply: %q %v", again, err)
	}
}

func TestDeleteUnknownIDRejected(t *testing.T) {
	_, x := newExec()
	_, err := run(t, x, Action{Type: TypeDeleteExpense, ID: 42})
	var ue *UserError
	if !errors.As(err, &ue) || !strings.Contains(ue.Msg, "42") {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteWithBadIDRejected(t *testing.T) {
	_, x := newExec()
	for _, id := range []float64{0, -3, 1.5, math.NaN()} {
		_, err := run(t, x, Action{Type: TypeDeleteExpense, ID: Num(id)})
		var ue *UserError
		if !errors.As(err, &ue) {
			t.Fatalf("id %v: got %v", id, err)
		}
	}
}

func TestDuplicateDeleteIsDeduped(t *testing.T) {
	_, x := newExec()
	run(t, x, Action{Type: TypeAddExpense, Amount: 5, Category: "food"})
	out, err := run(t, x, Action{Type: TypeDeleteExpense, ID: 1}, Action{Type: TypeDeleteExpense, ID: 1})
	if err != nil || len(out.Pending) != 1 {
		t.Fatalf("pending=%v err=%v", out.Pending, err)
	}
}

func TestCompleteReminder(t *testing.T) {
	m, x := newExec()
	run(t, x, Action{Type: TypeAddReminder, Title: "pay rent", Date: "tomorrow"})
	out, err := run(t, x, Action{Type: TypeCompleteReminder, ID: 1})
	if err != nil || !out.Changed {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	r, _ := m.GetReminder(context.Background(), 1)
	if !r.Done {
		t.Fatal("not done")
	}
	out, _ = run(t, x, Action{Type: TypeCompleteReminder, ID: 1})
	if out.Changed || !strings.Contains(out.Reply, "already") {
		t.Fatalf("second complete: %+v", out)
	}
}

func TestReminderTimeAlreadyPassedToday(t *testing.T) {
	m, x := newExec()
	_, err := run(t, x, Action{Type: TypeAddReminder, Title: "stand-up", Date: "today", Time: "09:00"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
	rem, _ := m.ListReminders(context.Background(), false, 10)
	if len(rem) != 0 {
		t.Fatal("stored a reminder in the past")
	}
}

func TestReminderRequiresTitle(t *testing.T) {
	_, x := newExec()
	_, err := run(t, x, Action{Type: TypeAddReminder, Date: "tomorrow"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestReminderTitleFallsBackToNote(t *testing.T) {
	m, x := newExec()
	if _, err := run(t, x, Action{Type: TypeAddReminder, Note: "buy milk", Date: "tomorrow"}); err != nil {
		t.Fatal(err)
	}
	rem, _ := m.ListReminders(context.Background(), true, 10)
	if rem[0].Title != "buy milk" {
		t.Fatalf("title %q", rem[0].Title)
	}
}

func TestClarifyWritesNothing(t *testing.T) {
	m, x := newExec()
	out, err := run(t, x,
		Action{Type: TypeAddExpense, Amount: 100, Category: "food"},
		Action{Type: TypeClarify, Say: "How much did you spend?"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reply != "How much did you spend?" || out.Changed {
		t.Fatalf("out=%+v", out)
	}
	if len(expenses(t, m)) != 0 {
		t.Fatal("clarify did not stop the other actions")
	}
}

func TestTooManyActions(t *testing.T) {
	_, x := newExec()
	acts := make([]Action, MaxActions+1)
	for i := range acts {
		acts[i] = Action{Type: TypeAddExpense, Amount: 1, Category: "food"}
	}
	_, err := run(t, x, acts...)
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestUnknownActionRejected(t *testing.T) {
	_, x := newExec()
	_, err := run(t, x, Action{Type: "drop_table"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestNoActionsUsesSay(t *testing.T) {
	_, x := newExec()
	out, err := x.Run(context.Background(), Plan{Say: "Hello there"}, fixedNow())
	if err != nil || out.Reply != "Hello there" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, _ = x.Run(context.Background(), Plan{}, fixedNow())
	if out.Reply == "" {
		t.Fatal("empty fallback reply")
	}
}

func TestListExpensesFiltersByCategory(t *testing.T) {
	_, x := newExec()
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"},
		Action{Type: TypeAddExpense, Amount: 80, Category: "transport", Note: "auto"},
	)
	out, err := run(t, x, Action{Type: TypeListExpenses, Period: "today", Category: "food"})
	if err != nil {
		t.Fatal(err)
	}
	if out.View == nil || len(out.View.Expenses) != 1 || out.View.Expenses[0].Note != "lunch" {
		t.Fatalf("view: %+v", out.View)
	}
	if !strings.Contains(out.Reply, "250 rupees") || strings.Contains(out.Reply, "80") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestListExpensesEmpty(t *testing.T) {
	_, x := newExec()
	out, err := run(t, x, Action{Type: TypeListExpenses, Period: "week"})
	if err != nil || out.Reply != "No expenses recorded in the last 7 days." {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestListExpensesBadPeriod(t *testing.T) {
	_, x := newExec()
	_, err := run(t, x, Action{Type: TypeListExpenses, Period: "decade"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestSummaryReply(t *testing.T) {
	_, x := newExec()
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 250, Category: "food"},
		Action{Type: TypeAddExpense, Amount: 100, Category: "food"},
		Action{Type: TypeAddExpense, Amount: 80, Category: "transport"},
		Action{Type: TypeAddReminder, Title: "pay rent", Date: "tomorrow"},
	)
	out, err := run(t, x, Action{Type: TypeSummary, Period: "month"})
	if err != nil {
		t.Fatal(err)
	}
	want := "This month you spent 430 rupees across 3 expenses. Most of it went on food, 350 rupees. You have 1 open reminder."
	if out.Reply != want {
		t.Fatalf("reply:\n got %q\nwant %q", out.Reply, want)
	}
	if out.View == nil || out.View.Summary == nil || out.View.Summary.Total != 430 {
		t.Fatalf("view: %+v", out.View)
	}
}

func TestSummaryEmptyDefaultsToToday(t *testing.T) {
	_, x := newExec()
	out, err := run(t, x, Action{Type: TypeSummary})
	if err != nil || out.Reply != "You haven't recorded any expenses today." {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestListRemindersMarksOverdue(t *testing.T) {
	m, x := newExec()
	m.AddReminder(context.Background(), models.ReminderInput{Title: "old", DueDate: "2026-11-01"}, fixedNow())
	out, err := run(t, x, Action{Type: TypeListReminders})
	if err != nil || !strings.Contains(out.Reply, "overdue") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestAnswer(t *testing.T) {
	yes := []string{"yes", "Yes.", "  YEAH!  ", "yes please", "go ahead", "haan", "okay", "yes yes", "yes delete the last one", "ok delete it", "Yes, delete that expense", "हाँ", "हां जी।", "हाँ हटाओ", "అవును", "సరే తొలగించు"}
	no := []string{"no", "No!", "cancel", "never mind", "don't", "nahi", "no no", "please don't delete it", "keep it please", "नहीं", "नहीं मत करो", "వద్దు", "కాదు"}
	other := []string{"", "add 50 for tea", "yes and add 50 for tea", "maybe", "what", "say yes or no", "yes wait no", "yes I think that is the right one to delete today"}
	for _, s := range yes {
		if y, n := Answer(s); !y || n {
			t.Errorf("%q should be yes", s)
		}
	}
	for _, s := range no {
		if y, n := Answer(s); y || !n {
			t.Errorf("%q should be no", s)
		}
	}
	for _, s := range other {
		if y, n := Answer(s); y || n {
			t.Errorf("%q should be neither", s)
		}
	}
}

func TestNumUnmarshal(t *testing.T) {
	var a Action
	if err := json.Unmarshal([]byte(`{"type":"add_expense","amount":"1,250","id":"7"}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Amount != 1250 || a.ID != 7 {
		t.Fatalf("got %+v", a)
	}
	if err := json.Unmarshal([]byte(`{"amount":null}`), &a); err != nil || a.Amount != 0 {
		t.Fatalf("null: %+v %v", a, err)
	}
	if err := json.Unmarshal([]byte(`{"amount":"two hundred"}`), &a); err != nil || !math.IsNaN(float64(a.Amount)) {
		t.Fatalf("text: %+v %v", a, err)
	}
}

func TestNormaliseTime(t *testing.T) {
	cases := map[string]string{
		"6 PM": "18:00", "6 pm": "18:00", "6:30 pm": "18:30", "18:00": "18:00",
		"7:05": "07:05", "12 AM": "00:00", "": "", "soon": "SOON",
	}
	for in, want := range cases {
		if got := normaliseTime(in); got != want {
			t.Errorf("normaliseTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveDate(t *testing.T) {
	now := fixedNow()
	cases := map[string]string{
		"": "2026-11-03", "today": "2026-11-03", "Yesterday": "2026-11-02",
		"tomorrow": "2026-11-04", "2026-12-25": "2026-12-25",
	}
	for in, want := range cases {
		if got := resolveDate(in, now); got != want {
			t.Errorf("resolveDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMoneyFormatting(t *testing.T) {
	cases := map[float64]string{1: "1 rupee", 250: "250 rupees", 99.5: "99.50 rupees", 0.99: "0.99 rupees"}
	for in, want := range cases {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
}
