package actions

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"tellr/confirm"
	"tellr/models"
)

const (
	TypeAddExpense       = "add_expense"
	TypeAddReminder      = "add_reminder"
	TypeListExpenses     = "list_expenses"
	TypeListReminders    = "list_reminders"
	TypeSummary          = "summary"
	TypeDeleteExpense    = "delete_expense"
	TypeDeleteReminder   = "delete_reminder"
	TypeCompleteReminder = "complete_reminder"
	TypeUpdateExpense    = "update_expense"
	TypeUpdateReminder   = "update_reminder"
	TypeSetBudget        = "set_budget"
	TypeClearBudget      = "clear_budget"
	TypeListBudgets      = "list_budgets"
	TypeRenameBudget     = "rename_budget"
	TypeClarify          = "clarify"

	MaxActions = 5
)

type Repo interface {
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

type TxFunc func(ctx context.Context, fn func(Repo) error) error

type Num float64

func (n *Num) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*n = 0
		return nil
	}
	s = strings.ReplaceAll(strings.TrimSpace(strings.Trim(s, `"`)), ",", "")
	if s == "" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*n = Num(math.NaN())
		return nil
	}
	*n = Num(f)
	return nil
}

type Action struct {
	Type     string `json:"type"`
	Amount   Num    `json:"amount"`
	Category string `json:"category"`
	NewCat   string `json:"new_category"`
	Note     string `json:"note"`
	Title    string `json:"title"`
	Date     string `json:"date"`
	Time     string `json:"time"`
	Period   string `json:"period"`
	Query    string `json:"query"`
	From     string `json:"from"`
	To       string `json:"to"`
	Repeat   string `json:"repeat"`
	ID       Num    `json:"id"`
	Say      string `json:"say"`
}

type Plan struct {
	Actions []Action `json:"actions"`
	Say     string   `json:"say"`
}

type View struct {
	Kind      string                `json:"kind"`
	Title     string                `json:"title"`
	Expenses  []models.Expense      `json:"expenses,omitempty"`
	Reminders []models.Reminder     `json:"reminders,omitempty"`
	Summary   *models.Summary       `json:"summary,omitempty"`
	Budgets   []models.BudgetStatus `json:"budgets,omitempty"`
}

type Outcome struct {
	Reply   string
	Changed bool
	Done    []string
	View    *View
	Pending []confirm.Item
}

type UserError struct {
	Msg string
}

func (e *UserError) Error() string {
	return e.Msg
}

func userErr(msg string) *UserError {
	return &UserError{Msg: msg}
}
