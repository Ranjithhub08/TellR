package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"tellr/actions"
	"tellr/ai"
)

type fakeSpeaker struct {
	on    bool
	audio []byte
	err   error
	text  string
	voice string
}

func (f *fakeSpeaker) Enabled() bool { return f.on }

func (f *fakeSpeaker) Speak(ctx context.Context, text, voice string) ([]byte, error) {
	f.text, f.voice = text, voice
	return f.audio, f.err
}

func TestSpeakReturnsAudio(t *testing.T) {
	e := newEnv(t)
	sp := &fakeSpeaker{on: true, audio: []byte("RIFFxx")}
	e.srv.Speaker = sp
	rec := e.do("POST", "/api/speak", `{"text":" Added 250 rupees. ","voice":"daniel"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/wav" || rec.Body.String() != "RIFFxx" {
		t.Fatalf("speak: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if sp.text != "Added 250 rupees." || sp.voice != "daniel" {
		t.Fatalf("passed: %q %q", sp.text, sp.voice)
	}
}

func TestSpeakValidation(t *testing.T) {
	e := newEnv(t)
	e.srv.Speaker = &fakeSpeaker{on: true, audio: []byte("x")}
	for _, body := range []string{`{"text":"  "}`, `{"text":"` + strings.Repeat("a", 221) + `"}`, `{oops`, `{"text":"hi","x":1}`} {
		if rec := e.do("POST", "/api/speak", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: %d", body, rec.Code)
		}
	}
}

func TestSpeakWhenDisabled(t *testing.T) {
	e := newEnv(t)
	if rec := e.do("POST", "/api/speak", `{"text":"hi"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil speaker: %d", rec.Code)
	}
	e.srv.Speaker = &fakeSpeaker{on: false}
	if rec := e.do("POST", "/api/speak", `{"text":"hi"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled speaker: %d", rec.Code)
	}
}

func TestSpeakErrorMapping(t *testing.T) {
	cases := map[error]int{
		ai.ErrBusy:          http.StatusServiceUnavailable,
		ai.ErrSpeechRefused: http.StatusServiceUnavailable,
		ai.ErrUnavailable:   http.StatusBadGateway,
		errors.New("other"): http.StatusBadGateway,
	}
	for err, want := range cases {
		e := newEnv(t)
		e.srv.Speaker = &fakeSpeaker{on: true, err: err}
		if rec := e.do("POST", "/api/speak", `{"text":"hi"}`); rec.Code != want {
			t.Errorf("%v: %d want %d", err, rec.Code, want)
		}
	}
}

func TestHealthReportsTTS(t *testing.T) {
	e := newEnv(t)
	e.srv.Speaker = &fakeSpeaker{on: true}
	rec := e.do("GET", "/api/health", "")
	if !strings.Contains(rec.Body.String(), `"tts":true`) || !strings.Contains(rec.Body.String(), "hannah") {
		t.Fatalf("health: %s", rec.Body.String())
	}
}

type fakeTranslator struct {
	out   string
	err   error
	calls int
	code  string
	text  string
}

func (f *fakeTranslator) Translate(ctx context.Context, text, code string) (string, error) {
	f.calls++
	f.code, f.text = code, text
	return f.out, f.err
}

func TestVoiceReplyIsTranslated(t *testing.T) {
	e := newEnv(t)
	tr := &fakeTranslator{out: "250 रुपये जोड़े गए।"}
	e.srv.Translator = tr
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: "add_expense", Amount: 250, Category: "food", Note: "lunch"}}}
	rec := e.do("POST", "/api/voice", `{"text":"मैंने 250 खर्च किए","lang":"hi-IN"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "रुपये जोड़े") || !strings.Contains(rec.Body.String(), `"reply_lang":"hi-IN"`) {
		t.Fatalf("hindi reply: %d %s", rec.Code, rec.Body.String())
	}
	if tr.code != "hi" || !strings.Contains(tr.text, "250") {
		t.Fatalf("translator saw %q %q", tr.code, tr.text)
	}
}

func TestEnglishAndUnknownLanguagesSkipTranslation(t *testing.T) {
	for _, lang := range []string{"en-IN", "", "fr-FR"} {
		e := newEnv(t)
		tr := &fakeTranslator{out: "x"}
		e.srv.Translator = tr
		e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: "add_expense", Amount: 10, Category: "food"}}}
		rec := e.do("POST", "/api/voice", `{"text":"ten on tea","lang":"`+lang+`"}`)
		if tr.calls != 0 || strings.Contains(rec.Body.String(), "reply_lang") {
			t.Errorf("%q translated: %s", lang, rec.Body.String())
		}
	}
}

func TestTranslationFailureFallsBackToEnglish(t *testing.T) {
	e := newEnv(t)
	e.srv.Translator = &fakeTranslator{err: errors.New("down")}
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: "add_expense", Amount: 10, Category: "food"}}}
	rec := e.do("POST", "/api/voice", `{"text":"టీ కోసం 10 ఖర్చు","lang":"te-IN"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Added 10 rupees") || strings.Contains(rec.Body.String(), "reply_lang") {
		t.Fatalf("fallback: %d %s", rec.Code, rec.Body.String())
	}
}

func TestConfirmPromptAndDeleteReplyAreTranslated(t *testing.T) {
	e := newEnv(t)
	tr := &fakeTranslator{out: "తెలుగు సమాధానం"}
	e.srv.Translator = tr
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: "add_expense", Amount: 10, Category: "food"}}}
	e.do("POST", "/api/voice", `{"text":"ten on tea"}`)
	e.plan.plan = actions.Plan{Actions: []actions.Action{{Type: "delete_expense", ID: 1}}}
	rec := e.do("POST", "/api/voice", `{"text":"దాన్ని తొలగించు","lang":"te-IN"}`)
	if !strings.Contains(rec.Body.String(), `"prompt":"తెలుగు సమాధానం"`) {
		t.Fatalf("prompt: %s", rec.Body.String())
	}
	var resp struct {
		Confirm struct {
			Token string `json:"token"`
		} `json:"confirm"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	rec = e.do("POST", "/api/confirm", `{"token":"`+resp.Confirm.Token+`","accept":true,"lang":"te-IN"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reply_lang":"te-IN"`) {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
}

type fakeIndic struct {
	on    bool
	audio []byte
	err   error
	text  string
	lang  string
}

func (f *fakeIndic) Enabled() bool { return f.on }

func (f *fakeIndic) Speak(ctx context.Context, text, lang string) ([]byte, error) {
	f.text, f.lang = text, lang
	return f.audio, f.err
}

func TestSpeakRoutesIndianLanguagesToIndicVoice(t *testing.T) {
	e := newEnv(t)
	sp := &fakeSpeaker{on: true, audio: []byte("ENGLISH")}
	ind := &fakeIndic{on: true, audio: []byte("TELUGU")}
	e.srv.Speaker = sp
	e.srv.Indic = ind
	rec := e.do("POST", "/api/speak", `{"text":"నమస్కారం","lang":"te-IN"}`)
	if rec.Code != 200 || rec.Body.String() != "TELUGU" || ind.lang != "te" || sp.text != "" {
		t.Fatalf("code=%d body=%q lang=%q english=%q", rec.Code, rec.Body.String(), ind.lang, sp.text)
	}
	rec = e.do("POST", "/api/speak", `{"text":"hello","voice":"hannah"}`)
	if rec.Code != 200 || rec.Body.String() != "ENGLISH" || ind.text != "నమస్కారం" {
		t.Fatalf("english path: code=%d body=%q", rec.Code, rec.Body.String())
	}
	health := expect(t, e.do("GET", "/api/health", ""), 200)
	if health["indic"] != true {
		t.Fatalf("health: %v", health)
	}
}

func TestSpeakIndicUnavailableAndRefused(t *testing.T) {
	e := newEnv(t)
	e.srv.Speaker = &fakeSpeaker{on: true, audio: []byte("E")}
	expect(t, e.do("POST", "/api/speak", `{"text":"नमस्ते","lang":"hi"}`), 503)
	e.srv.Indic = &fakeIndic{on: true, err: ai.ErrSpeechRefused}
	m := expect(t, e.do("POST", "/api/speak", `{"text":"नमस्ते","lang":"hi"}`), 503)
	if s, _ := m["error"].(string); !strings.Contains(s, "Hindi and Telugu") {
		t.Fatalf("error text: %v", m)
	}
	e.srv.Indic = &fakeIndic{on: true, err: ai.ErrBusy}
	expect(t, e.do("POST", "/api/speak", `{"text":"नमस्ते","lang":"hi"}`), 503)
}
