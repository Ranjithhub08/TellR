package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	"tellr/models"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = models.ErrNotFound

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Repo struct {
	q querier
}

type Store struct {
	Repo
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	v := url.Values{}
	v.Add("_pragma", "busy_timeout(5000)")
	v.Add("_pragma", "journal_mode(WAL)")
	v.Add("_pragma", "foreign_keys(1)")
	dsn := "file:" + path + "?" + v.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{Repo: Repo{q: db}, db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('reminders') WHERE name = 'repeat'`).Scan(&n); err != nil {
		return fmt.Errorf("inspect reminders table: %w", err)
	}
	if n == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE reminders ADD COLUMN repeat TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add repeat column: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) WithTx(ctx context.Context, fn func(Repo) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(Repo{q: tx}); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func paiseToAmount(p int64) float64 {
	return float64(p) / 100
}

func (r Repo) AddExpense(ctx context.Context, in models.ExpenseInput, now time.Time) (models.Expense, error) {
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO expenses (amount_paise, category, note, spent_on, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.Paise(), in.Category, in.Note, in.SpentOn, stamp(now))
	if err != nil {
		return models.Expense{}, fmt.Errorf("insert expense: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Expense{}, fmt.Errorf("expense id: %w", err)
	}
	return r.GetExpense(ctx, id)
}

func (r Repo) GetExpense(ctx context.Context, id int64) (models.Expense, error) {
	var e models.Expense
	var paise int64
	var created string
	err := r.q.QueryRowContext(ctx,
		`SELECT id, amount_paise, category, note, spent_on, created_at FROM expenses WHERE id = ?`, id).
		Scan(&e.ID, &paise, &e.Category, &e.Note, &e.SpentOn, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Expense{}, ErrNotFound
	}
	if err != nil {
		return models.Expense{}, fmt.Errorf("get expense: %w", err)
	}
	e.Amount = paiseToAmount(paise)
	e.CreatedAt = parseStamp(created)
	return e, nil
}

func (r Repo) ListExpenses(ctx context.Context, from, to string, limit int) ([]models.Expense, error) {
	if limit <= 0 || limit > 5000 {
		limit = 100
	}
	query := `SELECT id, amount_paise, category, note, spent_on, created_at FROM expenses`
	var args []any
	switch {
	case from != "" && to != "":
		query += ` WHERE spent_on >= ? AND spent_on <= ?`
		args = append(args, from, to)
	case from != "":
		query += ` WHERE spent_on >= ?`
		args = append(args, from)
	case to != "":
		query += ` WHERE spent_on <= ?`
		args = append(args, to)
	}
	query += ` ORDER BY spent_on DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	defer rows.Close()

	out := []models.Expense{}
	for rows.Next() {
		var e models.Expense
		var paise int64
		var created string
		if err := rows.Scan(&e.ID, &paise, &e.Category, &e.Note, &e.SpentOn, &created); err != nil {
			return nil, fmt.Errorf("scan expense: %w", err)
		}
		e.Amount = paiseToAmount(paise)
		e.CreatedAt = parseStamp(created)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	return out, nil
}

func (r Repo) UpdateExpense(ctx context.Context, id int64, in models.ExpenseInput) (models.Expense, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE expenses SET amount_paise = ?, category = ?, note = ?, spent_on = ? WHERE id = ?`,
		in.Paise(), in.Category, in.Note, in.SpentOn, id)
	if err != nil {
		return models.Expense{}, fmt.Errorf("update expense: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return models.Expense{}, err
	}
	return r.GetExpense(ctx, id)
}

func (r Repo) DeleteExpense(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM expenses WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete expense: %w", err)
	}
	return requireAffected(res)
}

func (r Repo) AddReminder(ctx context.Context, in models.ReminderInput, now time.Time) (models.Reminder, error) {
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO reminders (title, due_date, due_time, repeat, done, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
		in.Title, in.DueDate, in.DueTime, in.Repeat, stamp(now))
	if err != nil {
		return models.Reminder{}, fmt.Errorf("insert reminder: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Reminder{}, fmt.Errorf("reminder id: %w", err)
	}
	return r.GetReminder(ctx, id)
}

func (r Repo) GetReminder(ctx context.Context, id int64) (models.Reminder, error) {
	var m models.Reminder
	var done int
	var created string
	err := r.q.QueryRowContext(ctx,
		`SELECT id, title, due_date, due_time, repeat, done, created_at FROM reminders WHERE id = ?`, id).
		Scan(&m.ID, &m.Title, &m.DueDate, &m.DueTime, &m.Repeat, &done, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Reminder{}, ErrNotFound
	}
	if err != nil {
		return models.Reminder{}, fmt.Errorf("get reminder: %w", err)
	}
	m.Done = done == 1
	m.CreatedAt = parseStamp(created)
	return m, nil
}

func (r Repo) ListReminders(ctx context.Context, openOnly bool, limit int) ([]models.Reminder, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, title, due_date, due_time, repeat, done, created_at FROM reminders`
	if openOnly {
		query += ` WHERE done = 0`
	}
	query += ` ORDER BY done ASC, due_date ASC, due_time ASC, id ASC LIMIT ?`

	rows, err := r.q.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	defer rows.Close()

	out := []models.Reminder{}
	for rows.Next() {
		var m models.Reminder
		var done int
		var created string
		if err := rows.Scan(&m.ID, &m.Title, &m.DueDate, &m.DueTime, &m.Repeat, &done, &created); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		m.Done = done == 1
		m.CreatedAt = parseStamp(created)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	return out, nil
}

func (r Repo) SetReminderDone(ctx context.Context, id int64, done bool) (models.Reminder, error) {
	flag := 0
	if done {
		flag = 1
	}
	res, err := r.q.ExecContext(ctx, `UPDATE reminders SET done = ? WHERE id = ?`, flag, id)
	if err != nil {
		return models.Reminder{}, fmt.Errorf("update reminder: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return models.Reminder{}, err
	}
	return r.GetReminder(ctx, id)
}

func (r Repo) UpdateReminder(ctx context.Context, id int64, in models.ReminderInput) (models.Reminder, error) {
	done := 0
	if in.Done {
		done = 1
	}
	res, err := r.q.ExecContext(ctx,
		`UPDATE reminders SET title = ?, due_date = ?, due_time = ?, repeat = ?, done = ? WHERE id = ?`,
		in.Title, in.DueDate, in.DueTime, in.Repeat, done, id)
	if err != nil {
		return models.Reminder{}, fmt.Errorf("update reminder: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return models.Reminder{}, err
	}
	return r.GetReminder(ctx, id)
}

func (r Repo) DeleteReminder(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM reminders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	return requireAffected(res)
}

func (r Repo) Summary(ctx context.Context, period, from, to, today string) (models.Summary, error) {
	s := models.Summary{Period: period, From: from, To: to, ByCategory: []models.CategoryTotal{}}

	rows, err := r.q.QueryContext(ctx,
		`SELECT category, SUM(amount_paise), COUNT(*) FROM expenses
		 WHERE spent_on >= ? AND spent_on <= ?
		 GROUP BY category ORDER BY SUM(amount_paise) DESC, category ASC`, from, to)
	if err != nil {
		return models.Summary{}, fmt.Errorf("summary by category: %w", err)
	}
	defer rows.Close()

	var totalPaise int64
	for rows.Next() {
		var c models.CategoryTotal
		var paise int64
		if err := rows.Scan(&c.Category, &paise, &c.Count); err != nil {
			return models.Summary{}, fmt.Errorf("scan summary: %w", err)
		}
		c.Total = paiseToAmount(paise)
		totalPaise += paise
		s.Count += c.Count
		s.ByCategory = append(s.ByCategory, c)
	}
	if err := rows.Err(); err != nil {
		return models.Summary{}, fmt.Errorf("summary by category: %w", err)
	}
	rows.Close()
	s.Total = paiseToAmount(totalPaise)

	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE done = 0`).Scan(&s.OpenReminders); err != nil {
		return models.Summary{}, fmt.Errorf("count open reminders: %w", err)
	}
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM reminders WHERE done = 0 AND due_date < ?`, today).Scan(&s.OverdueReminders); err != nil {
		return models.Summary{}, fmt.Errorf("count overdue reminders: %w", err)
	}
	return s, nil
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r Repo) SetBudget(ctx context.Context, in models.BudgetInput, now time.Time) (models.Budget, error) {
	paise := int64(math.Round(in.Limit * 100))
	if paise < 1 {
		return models.Budget{}, errors.New("budget must be at least 0.01")
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO budgets (category, limit_paise, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(category) DO UPDATE SET limit_paise = excluded.limit_paise, updated_at = excluded.updated_at`,
		in.Category, paise, stamp(now))
	if err != nil {
		return models.Budget{}, fmt.Errorf("save budget: %w", err)
	}
	return models.Budget{Category: in.Category, Limit: paiseToAmount(paise)}, nil
}

func (r Repo) ListBudgets(ctx context.Context) ([]models.Budget, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT category, limit_paise FROM budgets ORDER BY CASE category WHEN 'all' THEN 0 ELSE 1 END, category`)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer rows.Close()
	out := []models.Budget{}
	for rows.Next() {
		var b models.Budget
		var paise int64
		if err := rows.Scan(&b.Category, &paise); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		b.Limit = paiseToAmount(paise)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	return out, nil
}

func (r Repo) DeleteBudget(ctx context.Context, category string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM budgets WHERE category = ?`, category)
	if err != nil {
		return fmt.Errorf("delete budget: %w", err)
	}
	return requireAffected(res)
}
