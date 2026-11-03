package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tellr/models"
)

var testNow = time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func expenseIn(amount float64, category, note, date string) models.ExpenseInput {
	return models.ExpenseInput{Amount: amount, Category: category, Note: note, SpentOn: date}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestPing(t *testing.T) {
	s := openTemp(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExpenseLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	e, err := s.AddExpense(ctx, expenseIn(250.5, "food", "lunch", "2026-11-03"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == 0 || e.Amount != 250.5 || e.Category != "food" || e.Note != "lunch" || e.SpentOn != "2026-11-03" {
		t.Fatalf("add: %+v", e)
	}

	got, err := s.GetExpense(ctx, e.ID)
	if err != nil || got.Amount != 250.5 || got.Note != "lunch" {
		t.Fatalf("get: %+v %v", got, err)
	}

	upd, err := s.UpdateExpense(ctx, e.ID, expenseIn(300, "travel", "cab", "2026-11-02"))
	if err != nil || upd.Amount != 300 || upd.Category != "travel" || upd.Note != "cab" || upd.SpentOn != "2026-11-02" {
		t.Fatalf("update: %+v %v", upd, err)
	}

	if err := s.DeleteExpense(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetExpense(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := s.DeleteExpense(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.UpdateExpense(ctx, e.ID, expenseIn(1, "food", "", "2026-11-03")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestAmountsKeepPaisePrecision(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	for i := 0; i < 10; i++ {
		if _, err := s.AddExpense(ctx, expenseIn(0.1, "other", "", "2026-11-03"), testNow); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := s.Summary(ctx, "today", "2026-11-03", "2026-11-03", "2026-11-03")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 1 || sum.Count != 10 {
		t.Fatalf("summary: %+v", sum)
	}
}

func TestListExpensesOrderAndFilter(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	add := func(a float64, d string) models.Expense {
		e, err := s.AddExpense(ctx, expenseIn(a, "food", "", d), testNow)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a := add(10, "2026-11-01")
	b := add(20, "2026-11-03")
	c := add(30, "2026-11-03")

	all, err := s.ListExpenses(ctx, "", "", 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %v %v", all, err)
	}
	if all[0].ID != c.ID || all[1].ID != b.ID || all[2].ID != a.ID {
		t.Fatalf("order: %v", []int64{all[0].ID, all[1].ID, all[2].ID})
	}

	ranged, _ := s.ListExpenses(ctx, "2026-11-02", "2026-11-03", 100)
	if len(ranged) != 2 {
		t.Fatalf("range: %v", ranged)
	}
	from, _ := s.ListExpenses(ctx, "2026-11-03", "", 100)
	if len(from) != 2 {
		t.Fatalf("from: %v", from)
	}
	to, _ := s.ListExpenses(ctx, "", "2026-11-01", 100)
	if len(to) != 1 || to[0].ID != a.ID {
		t.Fatalf("to: %v", to)
	}
	limited, _ := s.ListExpenses(ctx, "", "", 1)
	if len(limited) != 1 || limited[0].ID != c.ID {
		t.Fatalf("limit: %v", limited)
	}
}

func TestListReturnsEmptySlicesNotNil(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	ex, err := s.ListExpenses(ctx, "", "", 10)
	if err != nil || ex == nil || len(ex) != 0 {
		t.Fatalf("expenses: %#v %v", ex, err)
	}
	rm, err := s.ListReminders(ctx, false, 10)
	if err != nil || rm == nil || len(rm) != 0 {
		t.Fatalf("reminders: %#v %v", rm, err)
	}
	sum, err := s.Summary(ctx, "month", "2026-11-01", "2026-11-03", "2026-11-03")
	if err != nil || sum.ByCategory == nil || sum.Total != 0 || sum.Count != 0 {
		t.Fatalf("summary: %#v %v", sum, err)
	}
}

func TestSummaryGroupsByCategory(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	for _, in := range []models.ExpenseInput{
		expenseIn(100, "food", "", "2026-11-03"),
		expenseIn(50, "food", "", "2026-11-02"),
		expenseIn(500, "travel", "", "2026-11-03"),
		expenseIn(999, "bills", "", "2026-10-01"),
	} {
		if _, err := s.AddExpense(ctx, in, testNow); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := s.Summary(ctx, "month", "2026-11-01", "2026-11-03", "2026-11-03")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 650 || sum.Count != 3 || len(sum.ByCategory) != 2 {
		t.Fatalf("summary: %+v", sum)
	}
	if sum.ByCategory[0].Category != "travel" || sum.ByCategory[0].Total != 500 || sum.ByCategory[1].Category != "food" || sum.ByCategory[1].Count != 2 {
		t.Fatalf("by category: %+v", sum.ByCategory)
	}
}

func TestReminderLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	m, err := s.AddReminder(ctx, models.ReminderInput{Title: "call mom", DueDate: "2026-11-04", DueTime: "18:00"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == 0 || m.Title != "call mom" || m.DueDate != "2026-11-04" || m.DueTime != "18:00" || m.Done {
		t.Fatalf("add: %+v", m)
	}

	done, err := s.SetReminderDone(ctx, m.ID, true)
	if err != nil || !done.Done {
		t.Fatalf("done: %+v %v", done, err)
	}
	undone, err := s.SetReminderDone(ctx, m.ID, false)
	if err != nil || undone.Done {
		t.Fatalf("undone: %+v %v", undone, err)
	}

	upd, err := s.UpdateReminder(ctx, m.ID, models.ReminderInput{Title: "call dad", DueDate: "2026-11-05", DueTime: ""})
	if err != nil || upd.Title != "call dad" || upd.DueDate != "2026-11-05" || upd.DueTime != "" {
		t.Fatalf("update: %+v %v", upd, err)
	}

	if err := s.DeleteReminder(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReminder(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if _, err := s.SetReminderDone(ctx, m.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("done missing: %v", err)
	}
	if err := s.DeleteReminder(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestListRemindersOrderAndOpenOnly(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	later, _ := s.AddReminder(ctx, models.ReminderInput{Title: "later", DueDate: "2026-11-09"}, testNow)
	soon, _ := s.AddReminder(ctx, models.ReminderInput{Title: "soon", DueDate: "2026-11-04", DueTime: "09:00"}, testNow)
	finished, _ := s.AddReminder(ctx, models.ReminderInput{Title: "finished", DueDate: "2026-11-03"}, testNow)
	if _, err := s.SetReminderDone(ctx, finished.ID, true); err != nil {
		t.Fatal(err)
	}

	open, err := s.ListReminders(ctx, true, 50)
	if err != nil || len(open) != 2 || open[0].ID != soon.ID || open[1].ID != later.ID {
		t.Fatalf("open: %+v %v", open, err)
	}
	all, err := s.ListReminders(ctx, false, 50)
	if err != nil || len(all) != 3 || all[2].ID != finished.ID {
		t.Fatalf("all: %+v %v", all, err)
	}
}

func TestSummaryCountsReminders(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	s.AddReminder(ctx, models.ReminderInput{Title: "old", DueDate: "2026-11-01"}, testNow)
	s.AddReminder(ctx, models.ReminderInput{Title: "today", DueDate: "2026-11-03"}, testNow)
	s.AddReminder(ctx, models.ReminderInput{Title: "future", DueDate: "2026-11-10"}, testNow)
	d, _ := s.AddReminder(ctx, models.ReminderInput{Title: "done", DueDate: "2026-10-01"}, testNow)
	s.SetReminderDone(ctx, d.ID, true)

	sum, err := s.Summary(ctx, "today", "2026-11-03", "2026-11-03", "2026-11-03")
	if err != nil {
		t.Fatal(err)
	}
	if sum.OpenReminders != 3 || sum.OverdueReminders != 1 {
		t.Fatalf("reminders: %+v", sum)
	}
}

func TestWithTxCommitsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	err := s.WithTx(ctx, func(r Repo) error {
		if _, err := r.AddExpense(ctx, expenseIn(10, "food", "a", "2026-11-03"), testNow); err != nil {
			return err
		}
		_, err := r.AddReminder(ctx, models.ReminderInput{Title: "r", DueDate: "2026-11-04"}, testNow)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err = s.WithTx(ctx, func(r Repo) error {
		if _, err := r.AddExpense(ctx, expenseIn(99, "food", "b", "2026-11-03"), testNow); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}

	ex, _ := s.ListExpenses(ctx, "", "", 10)
	rm, _ := s.ListReminders(ctx, false, 10)
	if len(ex) != 1 || ex[0].Note != "a" || len(rm) != 1 {
		t.Fatalf("after rollback: %+v %+v", ex, rm)
	}
}

func TestDataSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "keep.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddExpense(ctx, expenseIn(42, "food", "kept", "2026-11-03"), testNow); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	ex, err := s2.ListExpenses(ctx, "", "", 10)
	if err != nil || len(ex) != 1 || ex[0].Note != "kept" || ex[0].Amount != 42 {
		t.Fatalf("reopen: %+v %v", ex, err)
	}
}

func TestSchemaRejectsBadRows(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO expenses(amount_paise, category, note, spent_on, created_at) VALUES (0, 'food', '', '2026-11-03', 'x')`); err == nil {
		t.Fatal("zero amount accepted")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO reminders(title, due_date, due_time, done, created_at) VALUES ('x', '2026-11-03', '', 5, 'x')`); err == nil {
		t.Fatal("bad done flag accepted")
	}
}

func TestConcurrentWritesAreSafe(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	const n = 40
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := s.AddExpense(ctx, expenseIn(1, "food", "", "2026-11-03"), testNow)
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	sum, err := s.Summary(ctx, "today", "2026-11-03", "2026-11-03", "2026-11-03")
	if err != nil || sum.Count != n || sum.Total != n {
		t.Fatalf("concurrent: %+v %v", sum, err)
	}
}

func TestBudgetsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if got, err := s.ListBudgets(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
	if _, err := s.SetBudget(ctx, models.BudgetInput{Category: "food", Limit: 5000.5}, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBudget(ctx, models.BudgetInput{Category: "all", Limit: 20000}, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBudget(ctx, models.BudgetInput{Category: "food", Limit: 6000}, testNow); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListBudgets(ctx)
	if err != nil || len(got) != 2 || got[0].Category != "all" || got[1].Category != "food" || got[1].Limit != 6000 {
		t.Fatalf("list: %+v %v", got, err)
	}
	if err := s.DeleteBudget(ctx, "food"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBudget(ctx, "food"); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.SetBudget(ctx, models.BudgetInput{Category: "food", Limit: 0}, testNow); err == nil {
		t.Fatal("zero budget must fail")
	}
}

func TestReminderRepeatPersists(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	m, err := s.AddReminder(ctx, models.ReminderInput{Title: "rent", DueDate: "2026-11-04", Repeat: "monthly"}, testNow)
	if err != nil || m.Repeat != "monthly" {
		t.Fatalf("add: %+v %v", m, err)
	}
	list, _ := s.ListReminders(ctx, true, 10)
	if len(list) != 1 || list[0].Repeat != "monthly" {
		t.Fatalf("list: %+v", list)
	}
	upd, err := s.UpdateReminder(ctx, m.ID, models.ReminderInput{Title: "rent", DueDate: "2026-12-04", Repeat: "weekly"})
	if err != nil || upd.Repeat != "weekly" || upd.DueDate != "2026-12-04" {
		t.Fatalf("update: %+v %v", upd, err)
	}
}

func TestOpenMigratesOldRemindersTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE reminders (
		id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, due_date TEXT NOT NULL,
		due_time TEXT NOT NULL DEFAULT '', done INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0, 1)), created_at TEXT NOT NULL);
		INSERT INTO reminders (title, due_date, created_at) VALUES ('legacy', '2026-11-05', '2026-11-01T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	defer s.Close()
	list, err := s.ListReminders(context.Background(), false, 10)
	if err != nil || len(list) != 1 || list[0].Title != "legacy" || list[0].Repeat != "" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := s.AddReminder(context.Background(), models.ReminderInput{Title: "new", DueDate: "2026-11-06", Repeat: "daily"}, testNow); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}
