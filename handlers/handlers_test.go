package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"tellr/actions"
	"tellr/ai"
	"tellr/confirm"
	"tellr/internal/memrepo"
)

type testBackend struct {
	*memrepo.Repo
}

func (b testBackend) Tx(ctx context.Context, fn func(actions.Repo) error) error {
	return b.Repo.Tx(ctx, func(v memrepo.View) error { return fn(v) })
}

func (b testBackend) Ping(ctx context.Context) error { return nil }

type fakePlanner struct {
	plan     actions.Plan
	err      error
	calls    int
	last     ai.Request
	panicNow bool
}

func (f *fakePlanner) Plan(ctx context.Context, req ai.Request) (actions.Plan, error) {
	f.calls++
	f.last = req
	if f.panicNow {
		panic("boom")
	}
	return f.plan, f.err
}

type env struct {
	srv  *Server
	h    http.Handler
	plan *fakePlanner
	mem  *memrepo.Repo
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		plan: &fakePlanner{},
		mem:  memrepo.New(),
		now:  time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC),
	}
	static := http.FileServerFS(fstest.MapFS{"index.html": {Data: []byte("<h1>TellR</h1>")}})
	e.srv = &Server{
		DB:        testBackend{e.mem},
		Planner:   e.plan,
		AIEnabled: true,
		Model:     "test-model",
		Confirms:  confirm.New(2 * time.Minute),
		Static:    static,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:       func() time.Time { return e.now },
	}
	e.h = e.srv.Routes()
	return e
}

func (e *env) do(method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
	}
	return m
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d, body %s", rec.Code, status, rec.Body.String())
	}
	if status == http.StatusNoContent {
		return nil
	}
	return decode(t, rec)
}

func addLunch() actions.Plan {
	return actions.Plan{Actions: []actions.Action{{Type: actions.TypeAddExpense, Amount: 250, Category: "food", Note: "lunch"}}}
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	m := expect(t, e.do("GET", "/api/health", ""), 200)
	if m["status"] != "ok" || m["ai"] != true || m["db"] != true || m["model"] != "test-model" {
		t.Fatalf("health: %v", m)
	}
}

func TestVoiceAddsExpenseAndPassesContext(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	rec := e.do("POST", "/api/voice", `{"text":"I spent 250 on lunch","lang":"en-IN","tz":"Asia/Kolkata"}`)
	m := expect(t, rec, 200)
	if m["ok"] != true || m["changed"] != true || m["reply"] != "Added 250 rupees for lunch under food." {
		t.Fatalf("voice: %v", m)
	}
	if done, _ := m["done"].([]any); len(done) != 1 {
		t.Fatalf("done: %v", m["done"])
	}
	list := expect(t, e.do("GET", "/api/expenses", ""), 200)
	items := list["expenses"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["spent_on"] != "2026-11-03" {
		t.Fatalf("stored: %v", items)
	}
	if e.plan.last.Text != "I spent 250 on lunch" || e.plan.last.TZ != "Asia/Kolkata" {
		t.Fatalf("planner request: %+v", e.plan.last)
	}
}

func TestVoiceSendsRecentRecordsToPlanner(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"first"}`)
	e.do("POST", "/api/voice", `{"text":"second"}`)
	if len(e.plan.last.Expenses) != 1 || e.plan.last.Expenses[0].ID != 1 {
		t.Fatalf("context: %+v", e.plan.last.Expenses)
	}
}

func TestVoiceUsesBrowserTimezoneForToday(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC)
	e.plan.plan = addLunch()
	expect(t, e.do("POST", "/api/voice", `{"text":"x","tz":"Asia/Kolkata"}`), 200)
	list := expect(t, e.do("GET", "/api/expenses", ""), 200)
	if got := list["expenses"].([]any)[0].(map[string]any)["spent_on"]; got != "2026-11-04" {
		t.Fatalf("Kolkata date %v", got)
	}
}

func TestVoiceBadTimezoneFallsBack(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	expect(t, e.do("POST", "/api/voice", `{"text":"x","tz":"Mars/Base"}`), 200)
}

func TestVoiceRejectsBadRequests(t *testing.T) {
	e := newEnv(t)
	cases := map[string]string{
		"empty":         `{"text":"   "}`,
		"too long":      `{"text":"` + strings.Repeat("a", 501) + `"}`,
		"not json":      `hello`,
		"unknown field": `{"text":"x","admin":true}`,
		"trailing":      `{"text":"x"} {"text":"y"}`,
		"wrong type":    `{"text":5}`,
	}
	for name, body := range cases {
		rec := e.do("POST", "/api/voice", body)
		if rec.Code != 400 {
			t.Errorf("%s: status %d body %s", name, rec.Code, rec.Body.String())
		}
	}
	if e.plan.calls != 0 {
		t.Fatal("planner was called for a bad request")
	}
}

func TestVoiceBodyTooLarge(t *testing.T) {
	e := newEnv(t)
	body := `{"text":"` + strings.Repeat("a", maxBody+10) + `"}`
	if rec := e.do("POST", "/api/voice", body); rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestVoiceDeleteNeedsConfirmationThenWorks(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"lunch"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeDeleteExpense, ID: 1}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"delete the last expense"}`), 200)
	c, ok := m["confirm"].(map[string]any)
	if !ok || c["token"] == "" || m["changed"] != false {
		t.Fatalf("confirm: %v", m)
	}
	if n := len(expect(t, e.do("GET", "/api/expenses", ""), 200)["expenses"].([]any)); n != 1 {
		t.Fatal("deleted before confirmation")
	}
	token := c["token"].(string)
	done := expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":true}`), 200)
	if done["reply"] != "Deleted 250 rupees for lunch." || done["changed"] != true {
		t.Fatalf("confirm reply: %v", done)
	}
	if n := len(expect(t, e.do("GET", "/api/expenses", ""), 200)["expenses"].([]any)); n != 0 {
		t.Fatal("not deleted after confirmation")
	}
	expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":true}`), 410)
}

func TestVoiceYesConfirmsWithoutCallingAI(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"lunch"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeDeleteExpense, ID: 1}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"delete it"}`), 200)
	token := m["confirm"].(map[string]any)["token"].(string)
	calls := e.plan.calls
	done := expect(t, e.do("POST", "/api/voice", `{"text":"Yes!","confirm_token":"`+token+`"}`), 200)
	if done["reply"] != "Deleted 250 rupees for lunch." || e.plan.calls != calls {
		t.Fatalf("done=%v calls=%d->%d", done, calls, e.plan.calls)
	}
}

func TestVoiceNoCancelsDeletion(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"lunch"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeDeleteExpense, ID: 1}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"delete it"}`), 200)
	token := m["confirm"].(map[string]any)["token"].(string)
	res := expect(t, e.do("POST", "/api/voice", `{"text":"no","confirm_token":"`+token+`"}`), 200)
	if res["changed"] != false {
		t.Fatalf("res: %v", res)
	}
	if n := len(expect(t, e.do("GET", "/api/expenses", ""), 200)["expenses"].([]any)); n != 1 {
		t.Fatal("expense deleted after saying no")
	}
	expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":true}`), 410)
}

func TestVoiceUnrelatedSentenceDropsPendingToken(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"lunch"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeDeleteExpense, ID: 1}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"delete it"}`), 200)
	token := m["confirm"].(map[string]any)["token"].(string)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeListReminders}}}
	expect(t, e.do("POST", "/api/voice", `{"text":"what reminders do I have","confirm_token":"`+token+`"}`), 200)
	expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":true}`), 410)
	if n := len(expect(t, e.do("GET", "/api/expenses", ""), 200)["expenses"].([]any)); n != 1 {
		t.Fatal("expense deleted")
	}
}

func TestConfirmEndpointDecline(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.do("POST", "/api/voice", `{"text":"lunch"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeDeleteExpense, ID: 1}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"delete it"}`), 200)
	token := m["confirm"].(map[string]any)["token"].(string)
	expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":false}`), 200)
	expect(t, e.do("POST", "/api/confirm", `{"token":"`+token+`","accept":true}`), 410)
	expect(t, e.do("POST", "/api/confirm", `{"token":"","accept":true}`), 400)
}

func TestVoicePlannerErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		ok     any
	}{
		{ai.ErrBusy, 503, nil},
		{ai.ErrUnavailable, 502, nil},
		{ai.ErrNotConfigured, 503, nil},
		{ai.ErrBadOutput, 200, false},
		{context.DeadlineExceeded, 502, nil},
	}
	for _, c := range cases {
		e := newEnv(t)
		e.plan.err = c.err
		rec := e.do("POST", "/api/voice", `{"text":"tell me a joke"}`)
		m := expect(t, rec, c.status)
		if c.ok != nil && m["ok"] != c.ok {
			t.Errorf("%v: %v", c.err, m)
		}
		if c.status != 200 && m["error"] == "" {
			t.Errorf("%v: missing error text", c.err)
		}
	}
}

func TestVoiceWithoutAIKey(t *testing.T) {
	e := newEnv(t)
	e.srv.AIEnabled = false
	expect(t, e.do("POST", "/api/voice", `{"text":"tell me a joke"}`), 503)
	if e.plan.calls != 0 {
		t.Fatal("planner called without a key")
	}
}

func TestVoiceUserErrorIsSpokenNotFailed(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: actions.TypeAddExpense, Amount: -5}}}
	m := expect(t, e.do("POST", "/api/voice", `{"text":"x"}`), 200)
	if m["ok"] != false || !strings.Contains(m["reply"].(string), "couldn't add") {
		t.Fatalf("got %v", m)
	}
}

func TestVoicePanicIsContained(t *testing.T) {
	e := newEnv(t)
	e.plan.panicNow = true
	m := expect(t, e.do("POST", "/api/voice", `{"text":"x"}`), 500)
	if m["error"] == "" {
		t.Fatal("no error text")
	}
	expect(t, e.do("GET", "/api/health", ""), 200)
}

func TestVoiceGlobalRateLimit(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.srv.VoiceLimit = NewLimiter(60, 2)
	e.h = e.srv.Routes()
	expect(t, e.do("POST", "/api/voice", `{"text":"a"}`), 200)
	expect(t, e.do("POST", "/api/voice", `{"text":"b"}`), 200)
	rec := e.do("POST", "/api/voice", `{"text":"c"}`)
	expect(t, rec, 429)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After header")
	}
}

func TestVoicePerIPLimitOnlyBehindTrustedProxy(t *testing.T) {
	e := newEnv(t)
	e.plan.plan = addLunch()
	e.srv.IPLimit = NewIPLimiter(60, 1)

	e.srv.TrustProxy = false
	e.h = e.srv.Routes()
	for i := 0; i < 3; i++ {
		expect(t, e.do("POST", "/api/voice", `{"text":"a"}`, "X-Forwarded-For", "1.1.1.1"), 200)
	}

	e.srv.TrustProxy = true
	e.h = e.srv.Routes()
	expect(t, e.do("POST", "/api/voice", `{"text":"a"}`, "X-Forwarded-For", "spoofed, 9.9.9.9"), 200)
	expect(t, e.do("POST", "/api/voice", `{"text":"a"}`, "X-Forwarded-For", "different, 9.9.9.9"), 429)
	expect(t, e.do("POST", "/api/voice", `{"text":"a"}`, "X-Forwarded-For", "8.8.8.8"), 200)
}

func TestExpenseCRUD(t *testing.T) {
	e := newEnv(t)
	created := expect(t, e.do("POST", "/api/expenses", `{"amount":120.5,"category":"transport","note":"cab"}`), 201)
	if created["id"] != float64(1) || created["amount"] != 120.5 || created["spent_on"] != "2026-11-03" {
		t.Fatalf("created: %v", created)
	}
	updated := expect(t, e.do("PUT", "/api/expenses/1", `{"amount":99,"category":"food","note":"lunch","spent_on":"2026-11-02"}`), 200)
	if updated["amount"] != float64(99) || updated["spent_on"] != "2026-11-02" {
		t.Fatalf("updated: %v", updated)
	}
	expect(t, e.do("PUT", "/api/expenses/77", `{"amount":1}`), 404)
	expect(t, e.do("PUT", "/api/expenses/abc", `{"amount":1}`), 400)
	expect(t, e.do("DELETE", "/api/expenses/1", ""), 204)
	expect(t, e.do("DELETE", "/api/expenses/1", ""), 404)
	expect(t, e.do("DELETE", "/api/expenses/0", ""), 400)
}

func TestExpenseValidationOverHTTP(t *testing.T) {
	e := newEnv(t)
	bad := []string{
		`{"amount":0}`, `{"amount":-3}`, `{"amount":5,"spent_on":"2026-11-30"}`,
		`{"amount":5,"spent_on":"yesterday"}`, `{"amount":"5"}`, `{}`, `{"amount":5,"extra":1}`,
	}
	for _, body := range bad {
		if rec := e.do("POST", "/api/expenses", body); rec.Code != 400 {
			t.Errorf("%s: status %d", body, rec.Code)
		}
	}
}

func TestExpenseListFilters(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/expenses", `{"amount":10,"spent_on":"2026-11-01"}`)
	e.do("POST", "/api/expenses", `{"amount":20,"spent_on":"2026-11-02"}`)
	e.do("POST", "/api/expenses", `{"amount":30,"spent_on":"2026-11-03"}`)
	m := expect(t, e.do("GET", "/api/expenses?from=2026-11-02&to=2026-11-02", ""), 200)
	items := m["expenses"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["amount"] != float64(20) {
		t.Fatalf("filtered: %v", items)
	}
	m = expect(t, e.do("GET", "/api/expenses?limit=2", ""), 200)
	if len(m["expenses"].([]any)) != 2 {
		t.Fatal("limit ignored")
	}
	expect(t, e.do("GET", "/api/expenses?from=nope", ""), 400)
	expect(t, e.do("GET", "/api/expenses?to=2026-02-31", ""), 400)
}

func TestReminderLifecycle(t *testing.T) {
	e := newEnv(t)
	created := expect(t, e.do("POST", "/api/reminders", `{"title":"call mom","due_date":"2026-11-04","due_time":"18:00"}`), 201)
	if created["done"] != false || created["id"] != float64(1) {
		t.Fatalf("created: %v", created)
	}
	expect(t, e.do("POST", "/api/reminders", `{"title":"","due_date":"2026-11-04"}`), 400)
	expect(t, e.do("POST", "/api/reminders", `{"title":"x","due_date":"2026-11-01"}`), 400)
	done := expect(t, e.do("POST", "/api/reminders/1/done", `{"done":true}`), 200)
	if done["done"] != true {
		t.Fatalf("done: %v", done)
	}
	open := expect(t, e.do("GET", "/api/reminders?open=1", ""), 200)
	if len(open["reminders"].([]any)) != 0 {
		t.Fatal("done reminder listed as open")
	}
	all := expect(t, e.do("GET", "/api/reminders", ""), 200)
	if len(all["reminders"].([]any)) != 1 {
		t.Fatal("done reminder missing from full list")
	}
	expect(t, e.do("POST", "/api/reminders/1/done", `{"done":false}`), 200)
	expect(t, e.do("POST", "/api/reminders/9/done", `{"done":true}`), 404)
	expect(t, e.do("DELETE", "/api/reminders/1", ""), 204)
	expect(t, e.do("DELETE", "/api/reminders/1", ""), 404)
}

func TestSummaryEndpoint(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/expenses", `{"amount":250,"category":"food"}`)
	e.do("POST", "/api/expenses", `{"amount":100,"category":"transport","spent_on":"2026-10-20"}`)
	e.do("POST", "/api/reminders", `{"title":"x","due_date":"2026-11-03"}`)
	today := expect(t, e.do("GET", "/api/summary?period=today", ""), 200)
	if today["total"] != float64(250) || today["count"] != float64(1) || today["open_reminders"] != float64(1) {
		t.Fatalf("today: %v", today)
	}
	month := expect(t, e.do("GET", "/api/summary", ""), 200)
	if month["period"] != "month" || month["total"] != float64(250) {
		t.Fatalf("month default: %v", month)
	}
	week := expect(t, e.do("GET", "/api/summary?period=week", ""), 200)
	if week["from"] != "2026-10-28" {
		t.Fatalf("week: %v", week)
	}
	expect(t, e.do("GET", "/api/summary?period=decade", ""), 400)
}

func TestCategories(t *testing.T) {
	e := newEnv(t)
	m := expect(t, e.do("GET", "/api/categories", ""), 200)
	if list := m["categories"].([]any); len(list) < 5 || list[0] != "food" {
		t.Fatalf("categories: %v", list)
	}
}

func TestSecurityHeadersAndAPIFallbacks(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/api/health", "")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Permissions-Policy", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses should not be cached")
	}
	m := expect(t, e.do("GET", "/api/nothing", ""), 404)
	if m["error"] == "" {
		t.Fatal("API 404 should be JSON")
	}
	expect(t, e.do("DELETE", "/api/nothing", ""), 404)
}

func TestStaticIsServed(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "TellR") {
		t.Fatalf("static: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("static page missing CSP")
	}
}

func TestLimiterRefills(t *testing.T) {
	l := NewLimiter(60, 1)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }
	l.b.last = clock
	if ok, _ := l.Allow(); !ok {
		t.Fatal("first request blocked")
	}
	if ok, wait := l.Allow(); ok || wait <= 0 {
		t.Fatalf("second request: ok=%v wait=%v", ok, wait)
	}
	clock = clock.Add(1100 * time.Millisecond)
	if ok, _ := l.Allow(); !ok {
		t.Fatal("did not refill after a second")
	}
}

func TestIPLimiterIsBounded(t *testing.T) {
	l := NewIPLimiter(60, 1)
	for i := 0; i < maxTrackedIPs*2; i++ {
		l.Allow(strings.Repeat("x", 1) + time.Duration(i).String())
	}
	if len(l.m) > maxTrackedIPs {
		t.Fatalf("tracking %d addresses", len(l.m))
	}
}
