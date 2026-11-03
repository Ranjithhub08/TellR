package actions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"tellr/confirm"
	"tellr/models"
)

type Executor struct {
	Tx TxFunc
}

func (x Executor) Run(ctx context.Context, plan Plan, now time.Time) (Outcome, error) {
	if len(plan.Actions) == 0 {
		return Outcome{Reply: clipSay(firstNonEmpty(strings.TrimSpace(plan.Say), "I didn't catch that. Try saying: I spent 250 on lunch."))}, nil
	}
	if len(plan.Actions) > MaxActions {
		return Outcome{}, userErr(fmt.Sprintf("That is too many things at once. Please give me up to %d at a time.", MaxActions))
	}
	for _, a := range plan.Actions {
		if a.Type == TypeClarify {
			return Outcome{Reply: firstNonEmpty(strings.TrimSpace(a.Say), strings.TrimSpace(plan.Say), "Could you say that again with a bit more detail?")}, nil
		}
	}

	var out Outcome
	var parts []string
	err := x.Tx(ctx, func(r Repo) error {
		out = Outcome{}
		parts = nil
		for _, a := range plan.Actions {
			part, err := apply(ctx, r, a, now, &out)
			if err != nil {
				return err
			}
			if part != "" {
				parts = append(parts, part)
			}
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}

	if len(out.Pending) > 0 {
		labels := make([]string, len(out.Pending))
		for i, p := range out.Pending {
			labels[i] = p.Label
		}
		parts = append(parts, fmt.Sprintf("Delete %s? Say yes or no.", joinList(labels)))
	}
	out.Reply = strings.Join(parts, " ")
	if out.Reply == "" {
		out.Reply = "Done."
	}
	return out, nil
}

func ApplyDeletes(ctx context.Context, tx TxFunc, items []confirm.Item) (string, error) {
	var deleted []string
	missing := 0
	err := tx(ctx, func(r Repo) error {
		deleted = nil
		missing = 0
		for _, it := range items {
			var err error
			switch it.Kind {
			case confirm.KindExpense:
				err = r.DeleteExpense(ctx, it.ID)
			case confirm.KindReminder:
				err = r.DeleteReminder(ctx, it.ID)
			default:
				return userErr("I can't delete that kind of item.")
			}
			if errors.Is(err, models.ErrNotFound) {
				missing++
				continue
			}
			if err != nil {
				return err
			}
			deleted = append(deleted, it.Label)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	switch {
	case len(deleted) == 0:
		return "That was already gone, so nothing was deleted.", nil
	case missing > 0:
		return fmt.Sprintf("Deleted %s. The rest was already gone.", joinList(deleted)), nil
	}
	return fmt.Sprintf("Deleted %s.", joinList(deleted)), nil
}

func apply(ctx context.Context, r Repo, a Action, now time.Time, out *Outcome) (string, error) {
	today := now.Format(models.DateLayout)
	switch a.Type {
	case TypeAddExpense:
		in := models.ExpenseInput{
			Amount:   float64(a.Amount),
			Category: a.Category,
			Note:     a.Note,
			SpentOn:  resolveDate(a.Date, now),
		}
		rawCat := in.Category
		if err := in.Validate(now); err != nil {
			return "", userErr("I couldn't add that expense: " + speakable(err) + ".")
		}
		in.Category = KeepCustom(ctx, r, rawCat, in.Category)
		e, err := r.AddExpense(ctx, in, now)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		msg := "Added " + money(e.Amount)
		if e.Note != "" {
			msg += " for " + e.Note
		}
		msg += " under " + e.Category
		if when := speakDate(e.SpentOn, today); when != "today" {
			msg += " " + when
		}
		return msg + "." + insight(ctx, r, e, today) + budgetWarning(ctx, r, e, now), nil

	case TypeAddReminder:
		title := firstNonEmpty(a.Title, a.Note)
		in := models.ReminderInput{
			Title:   title,
			DueDate: resolveDate(a.Date, now),
			DueTime: normaliseTime(a.Time),
			Repeat:  a.Repeat,
		}
		if err := in.Validate(now, false); err != nil {
			return "", userErr("I couldn't set that reminder: " + speakable(err) + ".")
		}
		if in.DueDate == today && in.DueTime != "" && in.DueTime < now.Format(models.TimeLayout) {
			return "", userErr("That time has already passed today. Which day should I set the reminder for?")
		}
		m, err := r.AddReminder(ctx, in, now)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		msg := fmt.Sprintf("Reminder set: %s, %s", m.Title, reminderWhen(m, today))
		if m.Repeat != "" {
			msg += ", repeating " + m.Repeat
		}
		return msg + ".", nil

	case TypeListExpenses:
		period := strings.ToLower(strings.TrimSpace(a.Period))
		from, to := strings.TrimSpace(a.From), strings.TrimSpace(a.To)
		if from != "" {
			from = resolveDate(from, now)
		}
		if to != "" {
			to = resolveDate(to, now)
		}
		if (from != "" && !validDate(from)) || (to != "" && !validDate(to)) {
			return "", userErr("I didn't understand that date range. Try saying it like: from the 1st to the 15th.")
		}
		if from != "" && to != "" && from > to {
			from, to = to, from
		}
		cat := strings.ToLower(strings.TrimSpace(a.Category))
		query := strings.ToLower(strings.TrimSpace(a.Query))
		if cat != "" && !models.ValidCategory(cat) && KeepCustom(ctx, r, cat, "other") != NormalizeCat(cat) {
			query = strings.TrimSpace(cat + " " + query)
			cat = ""
		}
		switch {
		case from != "" || to != "":
			period = ""
		case period == "" && (cat != "" || query != ""):
		case period == "":
			period = "today"
		default:
			f, t, err := models.PeriodRange(period, now)
			if err != nil {
				return "", userErr("I can only look at today, the last 7 days, this month, or week 1 to 4 of this month.")
			}
			from, to = f, t
		}
		items, err := r.ListExpenses(ctx, from, to, 2000)
		if err != nil {
			return "", err
		}
		if cat != "" || query != "" {
			filtered := items[:0:0]
			for _, e := range items {
				if cat != "" && e.Category != cat {
					continue
				}
				if query != "" && !strings.Contains(strings.ToLower(e.Note), query) && !strings.Contains(e.Category, query) {
					continue
				}
				filtered = append(filtered, e)
			}
			items = filtered
		}
		if len(items) > 200 {
			items = items[:200]
		}
		out.Done = append(out.Done, a.Type)
		phrase := rangePhrase(period, from, to, today)
		title := "Expenses, " + phrase
		if period == "" && from == "" && to == "" {
			title = "Expenses, all time"
			phrase = "so far"
		}
		if cat != "" {
			title += ", " + cat
		}
		if query != "" {
			title += ", matching " + query
		}
		out.View = &View{Kind: "expenses", Title: title, Expenses: items}
		what := ""
		if cat != "" {
			what = cat + " "
		}
		if query != "" {
			what += query + " "
		}
		if len(items) == 0 {
			return fmt.Sprintf("No %sexpenses recorded %s.", what, phrase), nil
		}
		var total int64
		for _, e := range items {
			total += int64(math.Round(e.Amount * 100))
		}
		labels := []string{}
		for i, e := range items {
			if i == 3 {
				break
			}
			labels = append(labels, expenseLabel(e))
		}
		if more := len(items) - len(labels); more > 0 {
			labels = append(labels, fmt.Sprintf("%d more", more))
		}
		noun := plural(len(items), what+"expense", what+"expenses")
		return fmt.Sprintf("You have %s %s totalling %s: %s.", noun, phrase, money(float64(total)/100), joinList(labels)), nil

	case TypeListReminders:
		items, err := r.ListReminders(ctx, true, 50)
		if err != nil {
			return "", err
		}
		out.Done = append(out.Done, a.Type)
		out.View = &View{Kind: "reminders", Title: "Open reminders", Reminders: items}
		if len(items) == 0 {
			return "You have no open reminders.", nil
		}
		labels := []string{}
		for i, m := range items {
			if i == 4 {
				break
			}
			label := fmt.Sprintf("%s %s", m.Title, reminderWhen(m, today))
			if m.Repeat != "" {
				label += ", repeats " + m.Repeat
			}
			if m.DueDate < today {
				label += ", overdue"
			}
			labels = append(labels, label)
		}
		if more := len(items) - len(labels); more > 0 {
			labels = append(labels, fmt.Sprintf("%d more", more))
		}
		return fmt.Sprintf("You have %s: %s.", plural(len(items), "open reminder", "open reminders"), joinList(labels)), nil

	case TypeSummary:
		period := strings.ToLower(strings.TrimSpace(a.Period))
		if period == "" {
			period = "today"
		}
		if _, _, err := models.PeriodRange(period, now); err != nil {
			return "", userErr("I can only summarise today, the last 7 days, this month, or week 1 to 4 of this month.")
		}
		s, err := LoadSummary(ctx, r, period, now)
		if err != nil {
			return "", err
		}
		out.Done = append(out.Done, a.Type)
		out.View = &View{Kind: "summary", Title: "Summary, " + expensePeriod(period), Summary: &s}
		var msg string
		if s.Count == 0 {
			msg = fmt.Sprintf("You haven't recorded any expenses %s.", expensePeriod(period))
		} else {
			msg = fmt.Sprintf("%s you spent %s across %s.", summaryPeriod(period), money(s.Total), plural(s.Count, "expense", "expenses"))
			if len(s.ByCategory) > 0 {
				top := s.ByCategory[0]
				msg += fmt.Sprintf(" Most of it went on %s, %s.", top.Category, money(top.Total))
			}
			msg += comparison(s, period)
		}
		if s.OpenReminders > 0 {
			msg += fmt.Sprintf(" You have %s", plural(s.OpenReminders, "open reminder", "open reminders"))
			if s.OverdueReminders > 0 {
				msg += fmt.Sprintf(", %d overdue", s.OverdueReminders)
			}
			msg += "."
		}
		return msg, nil

	case TypeDeleteExpense:
		id, ok := toID(a.ID)
		if !ok {
			return "", userErr("I'm not sure which expense you mean. Could you say which one?")
		}
		e, err := r.GetExpense(ctx, id)
		if errors.Is(err, models.ErrNotFound) {
			return "", userErr(fmt.Sprintf("I couldn't find expense number %d.", id))
		}
		if err != nil {
			return "", err
		}
		addPending(out, confirm.Item{Kind: confirm.KindExpense, ID: id, Label: expenseLabel(e)})
		return "", nil

	case TypeDeleteReminder:
		id, ok := toID(a.ID)
		if !ok {
			return "", userErr("I'm not sure which reminder you mean. Could you say which one?")
		}
		m, err := r.GetReminder(ctx, id)
		if errors.Is(err, models.ErrNotFound) {
			return "", userErr(fmt.Sprintf("I couldn't find reminder number %d.", id))
		}
		if err != nil {
			return "", err
		}
		addPending(out, confirm.Item{Kind: confirm.KindReminder, ID: id, Label: "the reminder " + m.Title})
		return "", nil

	case TypeCompleteReminder:
		id, ok := toID(a.ID)
		if !ok {
			return "", userErr("I'm not sure which reminder you mean. Could you say which one?")
		}
		m, err := r.GetReminder(ctx, id)
		if errors.Is(err, models.ErrNotFound) {
			return "", userErr(fmt.Sprintf("I couldn't find reminder number %d.", id))
		}
		if err != nil {
			return "", err
		}
		if m.Done {
			return fmt.Sprintf("%s is already marked done.", m.Title), nil
		}
		updated, advanced, err := CompleteReminder(ctx, r, m, today)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		if advanced {
			return fmt.Sprintf("Marked %s as done. Next one is %s.", m.Title, reminderWhen(updated, today)), nil
		}
		return fmt.Sprintf("Marked %s as done.", m.Title), nil

	case TypeUpdateExpense:
		id, ok := toID(a.ID)
		if !ok {
			return "", userErr("I'm not sure which expense you mean. Could you say which one?")
		}
		e, err := r.GetExpense(ctx, id)
		if errors.Is(err, models.ErrNotFound) {
			return "", userErr(fmt.Sprintf("I couldn't find expense number %d.", id))
		}
		if err != nil {
			return "", err
		}
		in := models.ExpenseInput{Amount: e.Amount, Category: e.Category, Note: e.Note, SpentOn: e.SpentOn}
		changed := false
		if f := float64(a.Amount); math.IsNaN(f) {
			return "", userErr("I didn't catch the new amount. Could you say it again?")
		} else if f != 0 {
			in.Amount, changed = f, true
		}
		if c := strings.ToLower(strings.TrimSpace(a.Category)); c != "" {
			in.Category, changed = c, true
		}
		if n := strings.TrimSpace(a.Note); n != "" {
			in.Note, changed = n, true
		}
		if d := strings.TrimSpace(a.Date); d != "" {
			in.SpentOn, changed = resolveDate(d, now), true
		}
		if !changed {
			return "", userErr("What should I change about that expense?")
		}
		rawCat := in.Category
		if err := in.Validate(now); err != nil {
			return "", userErr("I couldn't change that expense: " + speakable(err) + ".")
		}
		in.Category = KeepCustom(ctx, r, rawCat, in.Category)
		u, err := r.UpdateExpense(ctx, id, in)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		return "Updated: " + expenseLabel(u) + " under " + u.Category + "." + budgetWarning(ctx, r, u, now), nil

	case TypeUpdateReminder:
		id, ok := toID(a.ID)
		if !ok {
			return "", userErr("I'm not sure which reminder you mean. Could you say which one?")
		}
		m, err := r.GetReminder(ctx, id)
		if errors.Is(err, models.ErrNotFound) {
			return "", userErr(fmt.Sprintf("I couldn't find reminder number %d.", id))
		}
		if err != nil {
			return "", err
		}
		in := models.ReminderInput{Title: m.Title, DueDate: m.DueDate, DueTime: m.DueTime, Repeat: m.Repeat, Done: m.Done}
		changed, moved := false, false
		if t := strings.TrimSpace(a.Title); t != "" {
			in.Title, changed = t, true
		}
		if d := strings.TrimSpace(a.Date); d != "" {
			in.DueDate, changed, moved = resolveDate(d, now), true, true
		}
		if t := strings.TrimSpace(a.Time); t != "" {
			in.DueTime, changed, moved = normaliseTime(t), true, true
		}
		if rep := strings.TrimSpace(a.Repeat); rep != "" {
			in.Repeat, changed = rep, true
		}
		if !changed {
			return "", userErr("What should I change about that reminder?")
		}
		if err := in.Validate(now, !moved); err != nil {
			return "", userErr("I couldn't change that reminder: " + speakable(err) + ".")
		}
		if moved && in.DueDate == today && in.DueTime != "" && in.DueTime < now.Format(models.TimeLayout) {
			return "", userErr("That time has already passed today. Which day should I set the reminder for?")
		}
		u, err := r.UpdateReminder(ctx, id, in)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		msg := fmt.Sprintf("Updated reminder: %s, %s", u.Title, reminderWhen(u, today))
		if u.Repeat != "" {
			msg += ", repeating " + u.Repeat
		}
		return msg + ".", nil

	case TypeSetBudget:
		in := models.BudgetInput{Category: a.Category, Limit: float64(firstPositive(a.Amount))}
		if math.IsNaN(float64(a.Amount)) {
			return "", userErr("I didn't catch the budget amount. Could you say it again?")
		}
		if err := in.Validate(); err != nil {
			return "", userErr("I couldn't set that budget: " + err.Error() + ".")
		}
		b, err := r.SetBudget(ctx, in, now)
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		out.View = nil
		msg := fmt.Sprintf("Budget set: %s a month for %s.", money(b.Limit), categoryLabel(b.Category))
		statuses, err := BudgetStatuses(ctx, r, now)
		if err == nil {
			for _, st := range statuses {
				if st.Category == b.Category && st.Spent > 0 {
					msg += fmt.Sprintf(" You have spent %s of it so far this month.", money(st.Spent))
				}
			}
		}
		return msg, nil

	case TypeClearBudget:
		cat := strings.ToLower(strings.TrimSpace(a.Category))
		if cat == "" || cat == "overall" || cat == "total" || cat == "everything" {
			cat = models.AllBudget
		}
		err := r.DeleteBudget(ctx, cat)
		if errors.Is(err, models.ErrNotFound) {
			return fmt.Sprintf("You don't have a %s budget to remove.", categoryLabel(cat)), nil
		}
		if err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		return fmt.Sprintf("Removed the %s budget.", categoryLabel(cat)), nil

	case TypeRenameBudget:
		from := strings.ToLower(strings.TrimSpace(a.Category))
		to := strings.ToLower(strings.TrimSpace(a.NewCat))
		if from == "" || to == "" {
			return "", userErr("Which budget should change, and to what category?")
		}
		if from == to {
			return "", userErr("That budget is already " + categoryLabel(to) + ".")
		}
		to = models.NormalizeName(to)
		if to != models.AllBudget && !models.ValidBudgetName(to) {
			return "", userErr("That budget name is not valid. Use 1 to 24 letters, numbers or spaces.")
		}
		list, err := r.ListBudgets(ctx)
		if err != nil {
			return "", err
		}
		var old *models.Budget
		for i := range list {
			if list[i].Category == to {
				return "", userErr("You already have a " + categoryLabel(to) + " budget. Remove it first, or just change its amount.")
			}
			if list[i].Category == from {
				old = &list[i]
			}
		}
		if old == nil {
			return "", userErr("You don't have a " + categoryLabel(from) + " budget to change.")
		}
		if _, err := r.SetBudget(ctx, models.BudgetInput{Category: to, Limit: old.Limit}, now); err != nil {
			return "", userErr("I couldn't move that budget: " + err.Error() + ".")
		}
		if err := r.DeleteBudget(ctx, from); err != nil {
			return "", err
		}
		out.Changed = true
		out.Done = append(out.Done, a.Type)
		return fmt.Sprintf("Done. The %s budget of %s a month is now a %s budget.", categoryLabel(from), money(old.Limit), categoryLabel(to)), nil

	case TypeListBudgets:
		statuses, err := BudgetStatuses(ctx, r, now)
		if err != nil {
			return "", err
		}
		out.Done = append(out.Done, a.Type)
		out.View = &View{Kind: "budgets", Title: "Budgets, this month", Budgets: statuses}
		if len(statuses) == 0 {
			return "You haven't set any budgets yet. Try saying: set a food budget of 5000.", nil
		}
		labels := []string{}
		for i, st := range statuses {
			if i == 4 {
				break
			}
			labels = append(labels, fmt.Sprintf("%s %d percent of %s", categoryLabel(st.Category), st.Percent, money(st.Limit)))
		}
		if more := len(statuses) - len(labels); more > 0 {
			labels = append(labels, fmt.Sprintf("%d more", more))
		}
		return "Budgets this month: " + joinList(labels) + ".", nil
	}
	return "", userErr("I didn't understand one part of that. Could you say it again?")
}

func addPending(out *Outcome, it confirm.Item) {
	for _, p := range out.Pending {
		if p.Kind == it.Kind && p.ID == it.ID {
			return
		}
	}
	out.Pending = append(out.Pending, it)
}

func toID(n Num) (int64, bool) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 1 || f > 1e15 || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func resolveDate(s string, now time.Time) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "today":
		return now.Format(models.DateLayout)
	case "yesterday":
		return now.AddDate(0, 0, -1).Format(models.DateLayout)
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format(models.DateLayout)
	}
	return s
}

var timeLayouts = []string{"15:04", "3:04 PM", "3:04PM", "3 PM", "3PM", "15"}

func normaliseTime(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format(models.TimeLayout)
		}
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstPositive(n Num) Num {
	if f := float64(n); math.IsNaN(f) || f < 0 {
		return 0
	}
	return n
}

func clipSay(s string) string {
	r := []rune(s)
	if len(r) <= 500 {
		return s
	}
	return strings.TrimSpace(string(r[:500]))
}
