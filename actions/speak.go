package actions

import (
	"fmt"
	"math"
	"strings"
	"time"

	"tellr/models"
)

func money(v float64) string {
	r := math.Round(v*100) / 100
	if r == 1 {
		return "1 rupee"
	}
	if r == math.Trunc(r) {
		return fmt.Sprintf("%d rupees", int64(r))
	}
	return fmt.Sprintf("%.2f rupees", r)
}

func speakDate(date, today string) string {
	d, err1 := time.Parse(models.DateLayout, date)
	t, err2 := time.Parse(models.DateLayout, today)
	if err1 != nil || err2 != nil {
		return date
	}
	switch int(d.Sub(t).Hours() / 24) {
	case 0:
		return "today"
	case -1:
		return "yesterday"
	case 1:
		return "tomorrow"
	}
	return "on " + d.Format("2 January")
}

func speakTime(hhmm string) string {
	t, err := time.Parse(models.TimeLayout, hhmm)
	if err != nil {
		return hhmm
	}
	return t.Format("3:04 PM")
}

func reminderWhen(m models.Reminder, today string) string {
	w := speakDate(m.DueDate, today)
	if m.DueTime != "" {
		w += " at " + speakTime(m.DueTime)
	}
	return w
}

func expensePeriod(period string) string {
	switch period {
	case "week":
		return "in the last 7 days"
	case "month":
		return "this month"
	}
	if n := weekLabel(period); n != "" {
		return "in " + n + " of this month"
	}
	return "today"
}

func summaryPeriod(period string) string {
	switch period {
	case "week":
		return "In the last 7 days"
	case "month":
		return "This month"
	}
	if n := weekLabel(period); n != "" {
		return "In " + n + " of this month"
	}
	return "Today"
}

func weekLabel(period string) string {
	switch period {
	case "week1":
		return "week 1"
	case "week2":
		return "week 2"
	case "week3":
		return "week 3"
	case "week4":
		return "week 4"
	}
	return ""
}

func speakable(err error) string {
	r := strings.NewReplacer("spent_on", "date", "due_date", "date", "due_time", "time")
	return r.Replace(err.Error())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func expenseLabel(e models.Expense) string {
	if e.Note != "" {
		return fmt.Sprintf("%s for %s", money(e.Amount), e.Note)
	}
	return fmt.Sprintf("%s on %s", money(e.Amount), e.Category)
}

func joinList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
}
