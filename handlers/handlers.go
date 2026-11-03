package handlers

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tellr/actions"
	"tellr/ai"
	"tellr/confirm"
	"tellr/models"
)

const (
	maxSpeechChars = 500
	voiceTimeout   = 12 * time.Second
)

type Backend interface {
	actions.Repo
	Tx(ctx context.Context, fn func(actions.Repo) error) error
	Ping(ctx context.Context) error
}

type Translator interface {
	Translate(ctx context.Context, text, code string) (string, error)
}

type Speaker interface {
	Enabled() bool
	Speak(ctx context.Context, text, voice string) ([]byte, error)
}

type Listener interface {
	Enabled() bool
	Transcribe(ctx context.Context, audio []byte, mime, hint string) (string, string, error)
}

type Server struct {
	DB         Backend
	Planner    ai.Planner
	Speaker    Speaker
	Translator Translator
	Listener   Listener
	AIEnabled  bool
	Model      string
	Confirms   *confirm.Store
	Static     http.Handler
	Log        *slog.Logger
	VoiceLimit *Limiter
	IPLimit    *IPLimiter
	TrustProxy bool
	Now        func() time.Time
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) executor() actions.Executor {
	return actions.Executor{Tx: s.DB.Tx}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/categories", s.categories)
	mux.HandleFunc("POST /api/speak", s.limitVoice(s.speak))
	mux.HandleFunc("POST /api/transcribe", s.limitVoice(s.transcribe))
	mux.HandleFunc("POST /api/voice", s.limitVoice(s.voice))
	mux.HandleFunc("POST /api/confirm", s.confirmHandler)
	mux.HandleFunc("GET /api/expenses", s.listExpenses)
	mux.HandleFunc("POST /api/expenses", s.createExpense)
	mux.HandleFunc("PUT /api/expenses/{id}", s.updateExpense)
	mux.HandleFunc("DELETE /api/expenses/{id}", s.deleteExpense)
	mux.HandleFunc("GET /api/reminders", s.listReminders)
	mux.HandleFunc("POST /api/reminders", s.createReminder)
	mux.HandleFunc("POST /api/reminders/{id}/done", s.markReminder)
	mux.HandleFunc("DELETE /api/reminders/{id}", s.deleteReminder)
	mux.HandleFunc("GET /api/summary", s.summary)
	mux.HandleFunc("GET /api/daily", s.daily)
	mux.HandleFunc("GET /api/budgets", s.listBudgets)
	mux.HandleFunc("PUT /api/budgets/{category}", s.putBudget)
	mux.HandleFunc("DELETE /api/budgets/{category}", s.deleteBudget)
	mux.HandleFunc("GET /api/export.csv", s.exportCSV)
	mux.HandleFunc("/", s.fallback)
	return recoverer(s.log(), requestLog(s.log(), securityHeaders(mux)))
}

func (s *Server) fallback(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		writeError(w, http.StatusNotFound, "That endpoint does not exist.")
		return
	}
	if s.Static == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	s.Static.ServeHTTP(w, r)
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Server) location(name string) *time.Location {
	loc, err := models.LoadLocation(name)
	if err != nil {
		loc, _ = models.LoadLocation("")
		if loc == nil {
			loc = time.UTC
		}
	}
	return loc
}

func (s *Server) nowFor(r *http.Request, tz string) time.Time {
	if tz == "" {
		tz = r.Header.Get("X-Timezone")
	}
	return s.now().In(s.location(tz))
}

func (s *Server) limitVoice(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.IPLimit != nil && s.TrustProxy {
			if ok, wait := s.IPLimit.Allow(clientIP(r, true)); !ok {
				tooMany(w, wait)
				return
			}
		}
		if s.VoiceLimit != nil {
			if ok, wait := s.VoiceLimit.Allow(); !ok {
				tooMany(w, wait)
				return
			}
		}
		next(w, r)
	}
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(wait)))
	writeError(w, http.StatusTooManyRequests, "Too many requests. Please wait a few seconds and try again.")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := s.DB.Ping(ctx) == nil
	status := http.StatusOK
	if !dbOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status": map[bool]string{true: "ok", false: "degraded"}[dbOK],
		"db":     dbOK,
		"ai":     s.AIEnabled,
		"tts":    s.Speaker != nil && s.Speaker.Enabled(),
		"stt":    s.Listener != nil && s.Listener.Enabled(),
		"voices": ai.SpeechVoices,
		"model":  s.Model,
	})
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"categories": models.Categories})
}

type voiceRequest struct {
	Text         string        `json:"text"`
	Lang         string        `json:"lang"`
	TZ           string        `json:"tz"`
	ConfirmToken string        `json:"confirm_token"`
	History      []historyTurn `json:"history"`
}

type historyTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func cleanHistory(in []historyTurn) []ai.Turn {
	if len(in) > 6 {
		in = in[len(in)-6:]
	}
	out := make([]ai.Turn, 0, len(in))
	for _, t := range in {
		if t.Role != "user" && t.Role != "assistant" {
			continue
		}
		text := ai.SanitizeSpeech(t.Text)
		if text == "" {
			continue
		}
		out = append(out, ai.Turn{Role: t.Role, Text: text})
	}
	return out
}

type confirmInfo struct {
	Token     string         `json:"token"`
	Prompt    string         `json:"prompt"`
	ExpiresAt time.Time      `json:"expires_at"`
	Items     []confirm.Item `json:"items"`
}

type voiceResponse struct {
	OK        bool          `json:"ok"`
	Reply     string        `json:"reply"`
	ReplyLang string        `json:"reply_lang,omitempty"`
	Changed   bool          `json:"changed"`
	Done      []string      `json:"done"`
	Confirm   *confirmInfo  `json:"confirm,omitempty"`
	View      *actions.View `json:"view,omitempty"`
}

func spokenLang(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return ai.DetectLang(text)
}

func langCode(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(l, "-_"); i >= 0 {
		l = l[:i]
	}
	if ai.Translatable(l) {
		return l
	}
	return ""
}

func (s *Server) reply(ctx context.Context, w http.ResponseWriter, lang string, resp voiceResponse) {
	code := langCode(lang)
	if code != "" && s.Translator != nil && strings.TrimSpace(resp.Reply) != "" {
		tctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		out, err := s.Translator.Translate(tctx, resp.Reply, code)
		cancel()
		if err != nil {
			s.log().Warn("translate failed", "lang", code, "err", err)
		} else {
			resp.Reply = out
			resp.ReplyLang = code + "-IN"
			if resp.Confirm != nil {
				c := *resp.Confirm
				c.Prompt = out
				resp.Confirm = &c
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	var req voiceRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "Say or type something first.")
		return
	}
	if utf8.RuneCountInString(text) > maxSpeechChars {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("That was too long. Keep it under %d characters.", maxSpeechChars))
		return
	}
	now := s.nowFor(r, req.TZ)
	req.Lang = spokenLang(text, req.Lang)

	ctx, cancel := context.WithTimeout(r.Context(), voiceTimeout)
	defer cancel()

	if req.ConfirmToken != "" {
		yes, no := actions.Answer(text)
		switch {
		case yes:
			s.finishConfirm(ctx, w, req.Lang, req.ConfirmToken, true)
			return
		case no:
			s.finishConfirm(ctx, w, req.Lang, req.ConfirmToken, false)
			return
		}
		s.Confirms.Drop(req.ConfirmToken)
	}

	if talk, ok := ai.SmallTalk(text, now); ok {
		s.reply(ctx, w, req.Lang, voiceResponse{OK: true, Reply: talk, Done: []string{}})
		return
	}

	if !s.AIEnabled || s.Planner == nil {
		writeError(w, http.StatusServiceUnavailable, "The AI key is not set on the server, so voice commands are off. The forms still work.")
		return
	}

	expenses, err := s.DB.ListExpenses(ctx, "", "", 10)
	if err != nil {
		s.log().Error("load expenses for context", "err", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong on the server. Nothing was changed.")
		return
	}
	reminders, err := s.DB.ListReminders(ctx, true, 10)
	if err != nil {
		s.log().Error("load reminders for context", "err", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong on the server. Nothing was changed.")
		return
	}

	start := time.Now()
	plan, err := s.Planner.Plan(ctx, ai.Request{
		Text:      text,
		TZ:        s.location(req.TZ).String(),
		Now:       now,
		Expenses:  expenses,
		Reminders: reminders,
		History:   cleanHistory(req.History),
	})
	aiMs := time.Since(start).Milliseconds()
	if err != nil {
		s.planFailed(ctx, w, req.Lang, err, aiMs)
		return
	}

	out, err := s.executor().Run(ctx, plan, now)
	if err != nil {
		var ue *actions.UserError
		if errors.As(err, &ue) {
			s.reply(ctx, w, req.Lang, voiceResponse{OK: false, Reply: ue.Msg, Done: []string{}})
			return
		}
		s.log().Error("run actions", "err", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong saving that. Nothing was changed.")
		return
	}

	resp := voiceResponse{OK: true, Reply: out.Reply, Changed: out.Changed, Done: out.Done, View: out.View}
	if resp.Done == nil {
		resp.Done = []string{}
	}
	if len(out.Pending) > 0 {
		token, exp, err := s.Confirms.Put(out.Pending)
		if err != nil {
			s.log().Error("store confirmation", "err", err)
			writeError(w, http.StatusInternalServerError, "Something went wrong. Nothing was deleted.")
			return
		}
		resp.Confirm = &confirmInfo{Token: token, Prompt: out.Reply, ExpiresAt: exp, Items: out.Pending}
	}
	s.log().Info("voice", "ai_ms", aiMs, "actions", len(plan.Actions), "done", len(out.Done), "pending", len(out.Pending))
	s.reply(ctx, w, req.Lang, resp)
}

func (s *Server) planFailed(ctx context.Context, w http.ResponseWriter, lang string, err error, aiMs int64) {
	s.log().Warn("planner failed", "err", err, "ai_ms", aiMs)
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "The AI key is not set on the server, so voice commands are off. The forms still work.")
	case errors.Is(err, ai.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "The AI is busy right now. Wait a few seconds and try again.")
	case errors.Is(err, ai.ErrBadOutput):
		s.reply(ctx, w, lang, voiceResponse{OK: false, Reply: "Sorry, I couldn't understand that. Please say it again.", Done: []string{}})
	default:
		writeError(w, http.StatusBadGateway, "The AI did not answer in time. Try again, or use the forms.")
	}
}

type confirmRequest struct {
	Token  string `json:"token"`
	Accept bool   `json:"accept"`
	Lang   string `json:"lang"`
}

func (s *Server) confirmHandler(w http.ResponseWriter, r *http.Request) {
	var req confirmRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	s.finishConfirm(ctx, w, req.Lang, req.Token, req.Accept)
}

func (s *Server) finishConfirm(ctx context.Context, w http.ResponseWriter, lang, token string, accept bool) {
	if !accept {
		s.Confirms.Drop(token)
		s.reply(ctx, w, lang, voiceResponse{OK: true, Reply: "Okay, I won't delete it.", Done: []string{}})
		return
	}
	items, ok := s.Confirms.Take(token)
	if !ok {
		writeError(w, http.StatusGone, "That confirmation has expired. Please ask again.")
		return
	}
	reply, err := actions.ApplyDeletes(ctx, s.DB.Tx, items)
	if err != nil {
		var ue *actions.UserError
		if errors.As(err, &ue) {
			s.reply(ctx, w, lang, voiceResponse{OK: false, Reply: ue.Msg, Done: []string{}})
			return
		}
		s.log().Error("apply deletes", "err", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong. Nothing was deleted.")
		return
	}
	s.reply(ctx, w, lang, voiceResponse{OK: true, Reply: reply, Changed: true, Done: []string{"delete"}})
}

func queryDate(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return "", true
	}
	t, err := time.Parse(models.DateLayout, v)
	if err != nil || t.Format(models.DateLayout) != v {
		writeError(w, http.StatusBadRequest, key+" must be a real date in YYYY-MM-DD format")
		return "", false
	}
	return v, true
}

func queryLimit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 || n > 500 {
		return 100
	}
	return n
}

func (s *Server) listExpenses(w http.ResponseWriter, r *http.Request) {
	from, ok := queryDate(w, r, "from")
	if !ok {
		return
	}
	to, ok := queryDate(w, r, "to")
	if !ok {
		return
	}
	items, err := s.DB.ListExpenses(r.Context(), from, to, queryLimit(r))
	if err != nil {
		s.serverError(w, "list expenses", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"expenses": items})
}

func (s *Server) createExpense(w http.ResponseWriter, r *http.Request) {
	var in models.ExpenseInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := s.nowFor(r, "")
	if err := in.Validate(now); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.DB.AddExpense(r.Context(), in, now)
	if err != nil {
		s.serverError(w, "create expense", err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "id must be a positive number")
		return
	}
	var in models.ExpenseInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.Validate(s.nowFor(r, "")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.DB.UpdateExpense(r.Context(), id, in)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That expense no longer exists.")
		return
	}
	if err != nil {
		s.serverError(w, "update expense", err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "id must be a positive number")
		return
	}
	err := s.DB.DeleteExpense(r.Context(), id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That expense no longer exists.")
		return
	}
	if err != nil {
		s.serverError(w, "delete expense", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listReminders(w http.ResponseWriter, r *http.Request) {
	openOnly := r.URL.Query().Get("open") == "1"
	items, err := s.DB.ListReminders(r.Context(), openOnly, queryLimit(r))
	if err != nil {
		s.serverError(w, "list reminders", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": items})
}

func (s *Server) createReminder(w http.ResponseWriter, r *http.Request) {
	var in models.ReminderInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := s.nowFor(r, "")
	if err := in.Validate(now, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m, err := s.DB.AddReminder(r.Context(), in, now)
	if err != nil {
		s.serverError(w, "create reminder", err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

type doneRequest struct {
	Done bool `json:"done"`
}

func (s *Server) markReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "id must be a positive number")
		return
	}
	var req doneRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	today := s.nowFor(r, "").Format(models.DateLayout)
	var m models.Reminder
	advanced := false
	err := s.DB.Tx(r.Context(), func(repo actions.Repo) error {
		cur, err := repo.GetReminder(r.Context(), id)
		if err != nil {
			return err
		}
		if req.Done && !cur.Done {
			m, advanced, err = actions.CompleteReminder(r.Context(), repo, cur, today)
			return err
		}
		m, err = repo.SetReminderDone(r.Context(), id, req.Done)
		return err
	})
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That reminder no longer exists.")
		return
	}
	if err != nil {
		s.serverError(w, "mark reminder", err)
		return
	}
	writeJSON(w, http.StatusOK, reminderResponse{Reminder: m, Advanced: advanced})
}

type reminderResponse struct {
	models.Reminder
	Advanced bool `json:"advanced"`
}

func (s *Server) deleteReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "id must be a positive number")
		return
	}
	err := s.DB.DeleteReminder(r.Context(), id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That reminder no longer exists.")
		return
	}
	if err != nil {
		s.serverError(w, "delete reminder", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period == "" {
		period = "month"
	}
	now := s.nowFor(r, "")
	if _, _, err := models.PeriodRange(period, now); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sum, err := actions.LoadSummary(r.Context(), s.DB, period, now)
	if err != nil {
		s.serverError(w, "summary", err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	s.log().Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong on the server.")
}

type speakRequest struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
}

const maxSpeakChars = 220

func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	var req speakRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if utf8.RuneCountInString(text) > maxSpeakChars {
		writeError(w, http.StatusBadRequest, "text is too long to speak")
		return
	}
	if s.Speaker == nil || !s.Speaker.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "The natural voice is not set up on the server.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	audio, err := s.Speaker.Speak(ctx, text, req.Voice)
	if err != nil {
		s.log().Warn("speak failed", "err", err)
		switch {
		case errors.Is(err, ai.ErrBusy):
			writeError(w, http.StatusServiceUnavailable, "The voice service is busy.")
		case errors.Is(err, ai.ErrSpeechRefused):
			writeError(w, http.StatusServiceUnavailable, "The voice model was refused. Open the Groq console, try the Orpheus voice model once and accept its terms.")
		default:
			writeError(w, http.StatusBadGateway, "The voice service did not answer.")
		}
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	w.WriteHeader(http.StatusOK)
	w.Write(audio)
}

func (s *Server) listBudgets(w http.ResponseWriter, r *http.Request) {
	items, err := actions.BudgetStatuses(r.Context(), s.DB, s.nowFor(r, ""))
	if err != nil {
		s.serverError(w, "list budgets", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"budgets": items})
}

type budgetRequest struct {
	Limit float64 `json:"limit"`
}

func (s *Server) putBudget(w http.ResponseWriter, r *http.Request) {
	var req budgetRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := models.BudgetInput{Category: r.PathValue("category"), Limit: req.Limit}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := s.DB.SetBudget(r.Context(), in, s.nowFor(r, ""))
	if err != nil {
		s.serverError(w, "set budget", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) deleteBudget(w http.ResponseWriter, r *http.Request) {
	cat := strings.ToLower(strings.TrimSpace(r.PathValue("category")))
	if cat != models.AllBudget && !models.ValidCategory(cat) {
		writeError(w, http.StatusBadRequest, "That budget category does not exist.")
		return
	}
	err := s.DB.DeleteBudget(r.Context(), cat)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That budget no longer exists.")
		return
	}
	if err != nil {
		s.serverError(w, "delete budget", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func csvSafe(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	from, ok := queryDate(w, r, "from")
	if !ok {
		return
	}
	to, ok := queryDate(w, r, "to")
	if !ok {
		return
	}
	items, err := s.DB.ListExpenses(r.Context(), from, to, 5000)
	if err != nil {
		s.serverError(w, "export expenses", err)
		return
	}
	name := "tellr-expenses-" + s.nowFor(r, "").Format(models.DateLayout) + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "date", "category", "amount", "note"})
	for _, e := range items {
		cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.SpentOn,
			csvSafe(e.Category),
			strconv.FormatFloat(e.Amount, 'f', 2, 64),
			csvSafe(e.Note),
		})
	}
	cw.Flush()
}

type dayTotal struct {
	Date  string  `json:"date"`
	Total float64 `json:"total"`
}

func (s *Server) daily(w http.ResponseWriter, r *http.Request) {
	days := 14
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 60 {
			writeError(w, http.StatusBadRequest, "days must be between 1 and 60")
			return
		}
		days = n
	}
	now := s.nowFor(r, "")
	from := now.AddDate(0, 0, -(days - 1)).Format(models.DateLayout)
	to := now.Format(models.DateLayout)
	items, err := s.DB.ListExpenses(r.Context(), from, to, 5000)
	if err != nil {
		s.serverError(w, "daily totals", err)
		return
	}
	paise := map[string]int64{}
	for _, e := range items {
		paise[e.SpentOn] += int64(math.Round(e.Amount * 100))
	}
	out := make([]dayTotal, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format(models.DateLayout)
		out = append(out, dayTotal{Date: d, Total: float64(paise[d]) / 100})
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": out})
}

const maxAudioBytes = 3 << 20

func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	if s.Listener == nil || !s.Listener.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "Speech recognition is not set up on the server. Pick a language instead of Auto.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAudioBytes)
	audio, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "That recording was too long.")
			return
		}
		writeError(w, http.StatusBadRequest, "Could not read the recording.")
		return
	}
	if len(audio) < 800 {
		writeError(w, http.StatusBadRequest, "That recording was empty.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	start := time.Now()
	text, lang, err := s.Listener.Transcribe(ctx, audio, r.Header.Get("Content-Type"), langHint(r.Header.Get("X-Lang-Hint")))
	if err != nil {
		s.log().Warn("transcribe failed", "err", err, "ms", time.Since(start).Milliseconds())
		switch {
		case errors.Is(err, ai.ErrNoSpeech):
			writeJSON(w, http.StatusOK, map[string]any{"text": "", "lang": ""})
		case errors.Is(err, ai.ErrBusy):
			writeError(w, http.StatusServiceUnavailable, "Speech recognition is busy. Try again in a few seconds.")
		case errors.Is(err, ai.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "Speech recognition is not set up on the server.")
		default:
			writeError(w, http.StatusBadGateway, "Speech recognition did not answer. Try again or type.")
		}
		return
	}
	s.log().Info("transcribed", "ms", time.Since(start).Milliseconds(), "lang", lang)
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "lang": lang})
}

func langHint(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hi", "hi-in":
		return "hi"
	case "te", "te-in":
		return "te"
	case "en", "en-in":
		return "en"
	}
	return ""
}
