package actions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tellr/models"
)

func TestBudgetWarningsAtEightyAndOver(t *testing.T) {
	_, x := newExec()
	out, err := run(t, x, Action{Type: TypeSetBudget, Category: "food", Amount: 1000})
	if err != nil || out.Reply != "Budget set: 1000 rupees a month for food." {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 700, Category: "food", Note: "dinner"})
	if strings.Contains(out.Reply, "budget") {
		t.Fatalf("no warning expected: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 150, Category: "food", Note: "snacks"})
	if !strings.Contains(out.Reply, "used 85 percent of your food budget") {
		t.Fatalf("reply: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 10, Category: "food", Note: "tea"})
	if strings.Contains(out.Reply, "percent of your food") {
		t.Fatalf("80 percent warning must fire once: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 200, Category: "food", Note: "party"})
	if !strings.Contains(out.Reply, "over your food budget: 1060 rupees spent of 1000 rupees") {
		t.Fatalf("reply: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 50, Category: "transport", Note: "auto"})
	if strings.Contains(out.Reply, "budget") {
		t.Fatalf("other category must not warn: %q", out.Reply)
	}
}

func TestOverallBudgetCountsEveryCategory(t *testing.T) {
	_, x := newExec()
	run(t, x, Action{Type: TypeSetBudget, Category: "all", Amount: 500})
	run(t, x, Action{Type: TypeAddExpense, Amount: 300, Category: "food", Note: "a"})
	out, _ := run(t, x, Action{Type: TypeAddExpense, Amount: 150, Category: "travel", Note: "b"})
	if !strings.Contains(out.Reply, "90 percent of your overall budget") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestBackdatedExpenseOutsideMonthDoesNotWarn(t *testing.T) {
	_, x := newExec()
	run(t, x, Action{Type: TypeSetBudget, Category: "food", Amount: 100})
	out, err := run(t, x, Action{Type: TypeAddExpense, Amount: 500, Category: "food", Note: "old", Date: "2026-10-05"})
	if err != nil || strings.Contains(out.Reply, "budget") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestSetBudgetRejectsBadInput(t *testing.T) {
	_, x := newExec()
	for _, a := range []Action{
		{Type: TypeSetBudget, Category: "food"},
		{Type: TypeSetBudget, Category: "pets", Amount: 100},
		{Type: TypeSetBudget, Category: "food", Amount: -5},
	} {
		_, err := run(t, x, a)
		var ue *UserError
		if !errors.As(err, &ue) {
			t.Fatalf("%+v: got %v", a, err)
		}
	}
}

func TestClearAndListBudgets(t *testing.T) {
	_, x := newExec()
	out, _ := run(t, x, Action{Type: TypeListBudgets})
	if !strings.Contains(out.Reply, "haven't set any budgets") {
		t.Fatalf("reply: %q", out.Reply)
	}
	run(t, x, Action{Type: TypeSetBudget, Category: "food", Amount: 1000}, Action{Type: TypeSetBudget, Category: "all", Amount: 5000})
	run(t, x, Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"})
	out, _ = run(t, x, Action{Type: TypeListBudgets})
	if out.View == nil || out.View.Kind != "budgets" || len(out.View.Budgets) != 2 {
		t.Fatalf("view: %+v", out.View)
	}
	if out.View.Budgets[0].Category != "all" || out.View.Budgets[1].Percent != 25 {
		t.Fatalf("budgets: %+v", out.View.Budgets)
	}
	out, _ = run(t, x, Action{Type: TypeClearBudget, Category: "food"})
	if out.Reply != "Removed the food budget." {
		t.Fatalf("reply: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeClearBudget, Category: "food"})
	if !strings.Contains(out.Reply, "don't have a food budget") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestInsightAfterRepeatedCategory(t *testing.T) {
	_, x := newExec()
	out, _ := run(t, x, Action{Type: TypeAddExpense, Amount: 200, Category: "food", Note: "lunch"})
	if strings.Contains(out.Reply, "That makes") {
		t.Fatalf("first expense has no insight: %q", out.Reply)
	}
	run(t, x, Action{Type: TypeAddExpense, Amount: 40, Category: "food", Note: "tea"})
	out, _ = run(t, x, Action{Type: TypeAddExpense, Amount: 300, Category: "food", Note: "dinner"})
	if !strings.HasSuffix(out.Reply, "That makes 3 food expenses today, 540 rupees so far.") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestUpdateExpenseMergesFields(t *testing.T) {
	m, x := newExec()
	run(t, x, Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"})
	out, err := run(t, x, Action{Type: TypeUpdateExpense, ID: 1, Amount: 300})
	if err != nil || !out.Changed || !strings.HasPrefix(out.Reply, "Updated: 300 rupees for lunch under food.") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = run(t, x, Action{Type: TypeUpdateExpense, ID: 1, Category: "transport"})
	if err != nil || !strings.HasPrefix(out.Reply, "Updated: 300 rupees for lunch under transport.") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e := expenses(t, m)[0]
	if e.Amount != 300 || e.Category != "transport" || e.Note != "lunch" || e.SpentOn != "2026-11-03" {
		t.Fatalf("stored: %+v", e)
	}
}

func TestUpdateExpenseErrors(t *testing.T) {
	_, x := newExec()
	run(t, x, Action{Type: TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"})
	for _, a := range []Action{
		{Type: TypeUpdateExpense, ID: 1},
		{Type: TypeUpdateExpense, ID: 99, Amount: 5},
		{Type: TypeUpdateExpense, Amount: 5},
		{Type: TypeUpdateExpense, ID: 1, Date: "2999-01-01"},
	} {
		_, err := run(t, x, a)
		var ue *UserError
		if !errors.As(err, &ue) {
			t.Fatalf("%+v: got %v", a, err)
		}
	}
}

func TestUpdateReminderMergesFields(t *testing.T) {
	_, x := newExec()
	run(t, x, Action{Type: TypeAddReminder, Title: "call mom", Date: "tomorrow", Time: "18:00"})
	out, err := run(t, x, Action{Type: TypeUpdateReminder, ID: 1, Time: "19:30", Repeat: "weekly"})
	if err != nil || out.Reply != "Updated reminder: call mom, tomorrow at 7:30 PM, repeating weekly." {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = run(t, x, Action{Type: TypeUpdateReminder, ID: 1, Repeat: "none"})
	if err != nil || strings.Contains(out.Reply, "repeating") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	_, err = run(t, x, Action{Type: TypeUpdateReminder, ID: 1, Repeat: "hourly"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestRepeatingReminderAdvancesWhenDone(t *testing.T) {
	m, x := newExec()
	out, err := run(t, x, Action{Type: TypeAddReminder, Title: "pay rent", Date: "tomorrow", Time: "09:00", Repeat: "monthly"})
	if err != nil || !strings.Contains(out.Reply, "repeating monthly") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = run(t, x, Action{Type: TypeCompleteReminder, ID: 1})
	if err != nil || !strings.Contains(out.Reply, "Marked pay rent as done. Next one is on 4 December at 9:00 AM.") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	rm, _ := m.GetReminder(context.Background(), 1)
	if rm.Done || rm.DueDate != "2026-12-04" {
		t.Fatalf("stored: %+v", rm)
	}
}

func TestPlainReminderStaysDone(t *testing.T) {
	m, x := newExec()
	run(t, x, Action{Type: TypeAddReminder, Title: "call mom", Date: "tomorrow"})
	run(t, x, Action{Type: TypeCompleteReminder, ID: 1})
	rm, _ := m.GetReminder(context.Background(), 1)
	if !rm.Done {
		t.Fatalf("stored: %+v", rm)
	}
}

func TestAddReminderRejectsBadRepeat(t *testing.T) {
	_, x := newExec()
	_, err := run(t, x, Action{Type: TypeAddReminder, Title: "x", Date: "tomorrow", Repeat: "yearly"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestVoiceSearchByTextAndDates(t *testing.T) {
	_, x := newExec()
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 120, Category: "food", Note: "coffee beans", Date: "2026-10-20"},
		Action{Type: TypeAddExpense, Amount: 60, Category: "food", Note: "coffee"},
		Action{Type: TypeAddExpense, Amount: 900, Category: "shopping", Note: "shoes"},
	)
	out, err := run(t, x, Action{Type: TypeListExpenses, Query: "Coffee"})
	if err != nil || out.View == nil || len(out.View.Expenses) != 2 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if !strings.Contains(out.Reply, "2 coffee expenses so far totalling 180 rupees") {
		t.Fatalf("reply: %q", out.Reply)
	}
	out, _ = run(t, x, Action{Type: TypeListExpenses, From: "2026-10-01", To: "2026-10-31"})
	if len(out.View.Expenses) != 1 || out.View.Expenses[0].Note != "coffee beans" {
		t.Fatalf("view: %+v", out.View)
	}
	out, _ = run(t, x, Action{Type: TypeListExpenses, From: "2026-10-31", To: "2026-10-01"})
	if len(out.View.Expenses) != 1 {
		t.Fatalf("swapped range: %+v", out.View)
	}
	out, _ = run(t, x, Action{Type: TypeListExpenses, Category: "snacks"})
	if len(out.View.Expenses) != 0 {
		t.Fatalf("unknown category is text search: %+v", out.View)
	}
	_, err = run(t, x, Action{Type: TypeListExpenses, From: "last tuesday"})
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v", err)
	}
}

func TestSummaryComparesWithPreviousPeriod(t *testing.T) {
	_, x := newExec()
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 100, Category: "food", Note: "a", Date: "yesterday"},
		Action{Type: TypeAddExpense, Amount: 150, Category: "food", Note: "b"},
	)
	out, _ := run(t, x, Action{Type: TypeSummary, Period: "today"})
	if !strings.Contains(out.Reply, "That is 50 percent more than yesterday.") {
		t.Fatalf("reply: %q", out.Reply)
	}
	if out.View.Summary.Previous != 100 || !out.View.Summary.HasPrevious {
		t.Fatalf("summary: %+v", out.View.Summary)
	}
	out, _ = run(t, x, Action{Type: TypeSummary, Period: "week"})
	if strings.Contains(out.Reply, "percent") {
		t.Fatalf("no earlier week data, so no comparison: %q", out.Reply)
	}
}

func TestSummaryCompareLower(t *testing.T) {
	_, x := newExec()
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 400, Category: "food", Note: "a", Date: "yesterday"},
		Action{Type: TypeAddExpense, Amount: 100, Category: "food", Note: "b"},
	)
	out, _ := run(t, x, Action{Type: TypeSummary, Period: "today"})
	if !strings.Contains(out.Reply, "75 percent less than yesterday") {
		t.Fatalf("reply: %q", out.Reply)
	}
}

func TestBudgetStatusesUseMonthToDate(t *testing.T) {
	m, x := newExec()
	run(t, x, Action{Type: TypeSetBudget, Category: "food", Amount: 1000})
	run(t, x,
		Action{Type: TypeAddExpense, Amount: 400, Category: "food", Note: "a"},
		Action{Type: TypeAddExpense, Amount: 999, Category: "food", Note: "old", Date: "2026-10-30"},
	)
	st, err := BudgetStatuses(context.Background(), m, fixedNow())
	if err != nil || len(st) != 1 || st[0].Spent != 400 || st[0].Percent != 40 {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	_ = models.AllBudget
}
