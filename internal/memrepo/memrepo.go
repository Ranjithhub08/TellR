package memrepo

import (
	"context"
	"sort"
	"sync"
	"time"

	"tellr/models"
)

type state struct {
	expenses  []models.Expense
	reminders []models.Reminder
	budgets   map[string]float64
	nextExpID int64
	nextRemID int64
}

func (s *state) clone() *state {
	c := &state{nextExpID: s.nextExpID, nextRemID: s.nextRemID}
	c.expenses = append([]models.Expense(nil), s.expenses...)
	c.reminders = append([]models.Reminder(nil), s.reminders...)
	c.budgets = make(map[string]float64, len(s.budgets))
	for k, v := range s.budgets {
		c.budgets[k] = v
	}
	return c
}

type Repo struct {
	mu sync.Mutex
	st *state
}

func New() *Repo {
	return &Repo{st: &state{nextExpID: 1, nextRemID: 1, budgets: map[string]float64{}}}
}

type view struct {
	s *state
}

func (r *Repo) Tx(ctx context.Context, fn func(v View) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	work := r.st.clone()
	if err := fn(view{s: work}); err != nil {
		return err
	}
	r.st = work
	return nil
}

type View interface {
	AddExpense(ctx context.Context, in models.ExpenseInput, now time.Time) (models.Expense, error)
	GetExpense(ctx context.Context, id int64) (models.Expense, error)
	ListExpenses(ctx context.Context, from, to string, limit int) ([]models.Expense, error)
	UpdateExpense(ctx context.Context, id int64, in models.ExpenseInput) (models.Expense, error)
	DeleteExpense(ctx context.Context, id int64) error
	AddReminder(ctx context.Context, in models.ReminderInput, now time.Time) (models.Reminder, error)
	GetReminder(ctx context.Context, id int64) (models.Reminder, error)
	ListReminders(ctx context.Context, openOnly bool, limit int) ([]models.Reminder, error)
	SetReminderDone(ctx context.Context, id int64, done bool) (models.Reminder, error)
	UpdateReminder(ctx context.Context, id int64, in models.ReminderInput) (models.Reminder, error)
	DeleteReminder(ctx context.Context, id int64) error
	Summary(ctx context.Context, period, from, to, today string) (models.Summary, error)
	SetBudget(ctx context.Context, in models.BudgetInput, now time.Time) (models.Budget, error)
	ListBudgets(ctx context.Context) ([]models.Budget, error)
	DeleteBudget(ctx context.Context, category string) error
}

func (r *Repo) read(fn func(v view) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(view{s: r.st})
}

func (r *Repo) write(fn func(v view) error) error {
	return r.Tx(context.Background(), func(v View) error { return fn(v.(view)) })
}

func (r *Repo) AddExpense(ctx context.Context, in models.ExpenseInput, now time.Time) (e models.Expense, err error) {
	err = r.write(func(v view) error { e, err = v.AddExpense(ctx, in, now); return err })
	return
}

func (r *Repo) GetExpense(ctx context.Context, id int64) (e models.Expense, err error) {
	err = r.read(func(v view) error { e, err = v.GetExpense(ctx, id); return err })
	return
}

func (r *Repo) ListExpenses(ctx context.Context, from, to string, limit int) (out []models.Expense, err error) {
	err = r.read(func(v view) error { out, err = v.ListExpenses(ctx, from, to, limit); return err })
	return
}

func (r *Repo) UpdateExpense(ctx context.Context, id int64, in models.ExpenseInput) (e models.Expense, err error) {
	err = r.write(func(v view) error { e, err = v.UpdateExpense(ctx, id, in); return err })
	return
}

func (r *Repo) DeleteExpense(ctx context.Context, id int64) error {
	return r.write(func(v view) error { return v.DeleteExpense(ctx, id) })
}

func (r *Repo) AddReminder(ctx context.Context, in models.ReminderInput, now time.Time) (m models.Reminder, err error) {
	err = r.write(func(v view) error { m, err = v.AddReminder(ctx, in, now); return err })
	return
}

func (r *Repo) GetReminder(ctx context.Context, id int64) (m models.Reminder, err error) {
	err = r.read(func(v view) error { m, err = v.GetReminder(ctx, id); return err })
	return
}

func (r *Repo) ListReminders(ctx context.Context, openOnly bool, limit int) (out []models.Reminder, err error) {
	err = r.read(func(v view) error { out, err = v.ListReminders(ctx, openOnly, limit); return err })
	return
}

func (r *Repo) SetReminderDone(ctx context.Context, id int64, done bool) (m models.Reminder, err error) {
	err = r.write(func(v view) error { m, err = v.SetReminderDone(ctx, id, done); return err })
	return
}

func (r *Repo) UpdateReminder(ctx context.Context, id int64, in models.ReminderInput) (m models.Reminder, err error) {
	err = r.write(func(v view) error { m, err = v.UpdateReminder(ctx, id, in); return err })
	return
}

func (r *Repo) SetBudget(ctx context.Context, in models.BudgetInput, now time.Time) (b models.Budget, err error) {
	err = r.write(func(v view) error { b, err = v.SetBudget(ctx, in, now); return err })
	return
}

func (r *Repo) ListBudgets(ctx context.Context) (out []models.Budget, err error) {
	err = r.read(func(v view) error { out, err = v.ListBudgets(ctx); return err })
	return
}

func (r *Repo) DeleteBudget(ctx context.Context, category string) error {
	return r.write(func(v view) error { return v.DeleteBudget(ctx, category) })
}

func (r *Repo) DeleteReminder(ctx context.Context, id int64) error {
	return r.write(func(v view) error { return v.DeleteReminder(ctx, id) })
}

func (r *Repo) Summary(ctx context.Context, period, from, to, today string) (s models.Summary, err error) {
	err = r.read(func(v view) error { s, err = v.Summary(ctx, period, from, to, today); return err })
	return
}

func (v view) AddExpense(_ context.Context, in models.ExpenseInput, now time.Time) (models.Expense, error) {
	e := models.Expense{
		ID:        v.s.nextExpID,
		Amount:    float64(in.Paise()) / 100,
		Category:  in.Category,
		Note:      in.Note,
		SpentOn:   in.SpentOn,
		CreatedAt: now,
	}
	v.s.nextExpID++
	v.s.expenses = append(v.s.expenses, e)
	return e, nil
}

func (v view) GetExpense(_ context.Context, id int64) (models.Expense, error) {
	for _, e := range v.s.expenses {
		if e.ID == id {
			return e, nil
		}
	}
	return models.Expense{}, models.ErrNotFound
}

func (v view) ListExpenses(_ context.Context, from, to string, limit int) ([]models.Expense, error) {
	if limit <= 0 || limit > 5000 {
		limit = 100
	}
	out := []models.Expense{}
	for _, e := range v.s.expenses {
		if from != "" && e.SpentOn < from {
			continue
		}
		if to != "" && e.SpentOn > to {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SpentOn != out[j].SpentOn {
			return out[i].SpentOn > out[j].SpentOn
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (v view) UpdateExpense(_ context.Context, id int64, in models.ExpenseInput) (models.Expense, error) {
	for i := range v.s.expenses {
		if v.s.expenses[i].ID == id {
			v.s.expenses[i].Amount = float64(in.Paise()) / 100
			v.s.expenses[i].Category = in.Category
			v.s.expenses[i].Note = in.Note
			v.s.expenses[i].SpentOn = in.SpentOn
			return v.s.expenses[i], nil
		}
	}
	return models.Expense{}, models.ErrNotFound
}

func (v view) DeleteExpense(_ context.Context, id int64) error {
	for i, e := range v.s.expenses {
		if e.ID == id {
			v.s.expenses = append(v.s.expenses[:i], v.s.expenses[i+1:]...)
			return nil
		}
	}
	return models.ErrNotFound
}

func (v view) AddReminder(_ context.Context, in models.ReminderInput, now time.Time) (models.Reminder, error) {
	m := models.Reminder{
		ID:        v.s.nextRemID,
		Title:     in.Title,
		DueDate:   in.DueDate,
		DueTime:   in.DueTime,
		Repeat:    in.Repeat,
		CreatedAt: now,
	}
	v.s.nextRemID++
	v.s.reminders = append(v.s.reminders, m)
	return m, nil
}

func (v view) GetReminder(_ context.Context, id int64) (models.Reminder, error) {
	for _, m := range v.s.reminders {
		if m.ID == id {
			return m, nil
		}
	}
	return models.Reminder{}, models.ErrNotFound
}

func (v view) ListReminders(_ context.Context, openOnly bool, limit int) ([]models.Reminder, error) {
	if limit <= 0 || limit > 5000 {
		limit = 100
	}
	out := []models.Reminder{}
	for _, m := range v.s.reminders {
		if openOnly && m.Done {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Done != b.Done {
			return !a.Done
		}
		if a.DueDate != b.DueDate {
			return a.DueDate < b.DueDate
		}
		if a.DueTime != b.DueTime {
			return a.DueTime < b.DueTime
		}
		return a.ID < b.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (v view) SetReminderDone(_ context.Context, id int64, done bool) (models.Reminder, error) {
	for i := range v.s.reminders {
		if v.s.reminders[i].ID == id {
			v.s.reminders[i].Done = done
			return v.s.reminders[i], nil
		}
	}
	return models.Reminder{}, models.ErrNotFound
}

func (v view) UpdateReminder(_ context.Context, id int64, in models.ReminderInput) (models.Reminder, error) {
	for i := range v.s.reminders {
		if v.s.reminders[i].ID == id {
			m := &v.s.reminders[i]
			m.Title, m.DueDate, m.DueTime, m.Repeat, m.Done = in.Title, in.DueDate, in.DueTime, in.Repeat, in.Done
			return *m, nil
		}
	}
	return models.Reminder{}, models.ErrNotFound
}

func (v view) SetBudget(_ context.Context, in models.BudgetInput, _ time.Time) (models.Budget, error) {
	v.s.budgets[in.Category] = in.Limit
	return models.Budget{Category: in.Category, Limit: in.Limit}, nil
}

func (v view) ListBudgets(_ context.Context) ([]models.Budget, error) {
	out := []models.Budget{}
	for k, l := range v.s.budgets {
		out = append(out, models.Budget{Category: k, Limit: l})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Category == models.AllBudget) != (out[j].Category == models.AllBudget) {
			return out[i].Category == models.AllBudget
		}
		return out[i].Category < out[j].Category
	})
	return out, nil
}

func (v view) DeleteBudget(_ context.Context, category string) error {
	if _, ok := v.s.budgets[category]; !ok {
		return models.ErrNotFound
	}
	delete(v.s.budgets, category)
	return nil
}

func (v view) DeleteReminder(_ context.Context, id int64) error {
	for i, m := range v.s.reminders {
		if m.ID == id {
			v.s.reminders = append(v.s.reminders[:i], v.s.reminders[i+1:]...)
			return nil
		}
	}
	return models.ErrNotFound
}

func (v view) Summary(_ context.Context, period, from, to, today string) (models.Summary, error) {
	s := models.Summary{Period: period, From: from, To: to, ByCategory: []models.CategoryTotal{}}
	totals := map[string]*models.CategoryTotal{}
	paise := map[string]int64{}
	var all int64
	for _, e := range v.s.expenses {
		if e.SpentOn < from || e.SpentOn > to {
			continue
		}
		p := int64(e.Amount*100 + 0.5)
		c := totals[e.Category]
		if c == nil {
			c = &models.CategoryTotal{Category: e.Category}
			totals[e.Category] = c
		}
		c.Count++
		paise[e.Category] += p
		all += p
		s.Count++
	}
	for k, c := range totals {
		c.Total = float64(paise[k]) / 100
		s.ByCategory = append(s.ByCategory, *c)
	}
	sort.SliceStable(s.ByCategory, func(i, j int) bool {
		if s.ByCategory[i].Total != s.ByCategory[j].Total {
			return s.ByCategory[i].Total > s.ByCategory[j].Total
		}
		return s.ByCategory[i].Category < s.ByCategory[j].Category
	})
	s.Total = float64(all) / 100
	for _, m := range v.s.reminders {
		if m.Done {
			continue
		}
		s.OpenReminders++
		if m.DueDate < today {
			s.OverdueReminders++
		}
	}
	return s, nil
}
