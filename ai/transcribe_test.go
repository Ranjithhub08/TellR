package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestDetectLang(t *testing.T) {
	cases := map[string]string{
		"I spent 250 on lunch": "en",
		"":                     "en",
		"250 rupees":           "en",
		"मैंने लंच पर 250 खर्च किए":       "hi",
		"నేను లంచ్ కోసం 250 ఖర్చు చేశాను": "te",
		"hello నమస్కారం":                  "te",
		"hello नमस्ते":                    "hi",
	}
	for in, want := range cases {
		if got := DetectLang(in); got != want {
			t.Errorf("%q -> %s, want %s", in, got, want)
		}
	}
}

func TestTranscribeSendsMultipartAndDetects(t *testing.T) {
	var gotAuth, gotModel, gotFilename, gotPath string
	var gotAudio []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		f, hdr, err := r.FormFile("file")
		if err == nil {
			gotFilename = hdr.Filename
			gotAudio, _ = io.ReadAll(f)
		}
		if r.FormValue("language") == "hi" {
			w.Write([]byte(`{"text":"नहीं","language":"hindi","segments":[{"start":0,"end":2,"avg_logprob":-1.8,"no_speech_prob":0.1}]}`))
			return
		}
		w.Write([]byte(`{"text":" నేను లంచ్ కోసం 250 ఖర్చు చేశాను ","language":"telugu","segments":[{"start":0,"end":2,"avg_logprob":-0.2,"no_speech_prob":0.01}]}`))
	}))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "whisper-test")
	text, lang, err := tr.Transcribe(context.Background(), []byte("AUDIO"), "audio/webm;codecs=opus", "")
	if err != nil || lang != "te" || !strings.Contains(text, "250") {
		t.Fatalf("text=%q lang=%q err=%v", text, lang, err)
	}
	if gotAuth != "Bearer k" || gotModel != "whisper-test" || gotFilename != "speech.webm" || string(gotAudio) != "AUDIO" || gotPath != "/audio/transcriptions" {
		t.Fatalf("auth=%q model=%q file=%q audio=%q path=%q", gotAuth, gotModel, gotFilename, gotAudio, gotPath)
	}
}

func TestTranscribeErrors(t *testing.T) {
	status := http.StatusOK
	body := `{"text":""}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	ctx := context.Background()
	if _, _, err := tr.Transcribe(ctx, []byte("a"), "audio/webm", ""); !errors.Is(err, ErrNoSpeech) {
		t.Fatalf("empty: %v", err)
	}
	body = `{"text":"Thank you."}`
	if _, _, err := tr.Transcribe(ctx, []byte("a"), "audio/webm", ""); !errors.Is(err, ErrNoSpeech) {
		t.Fatalf("filler: %v", err)
	}
	status = http.StatusTooManyRequests
	if _, _, err := tr.Transcribe(ctx, []byte("a"), "audio/webm", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("429: %v", err)
	}
	status = http.StatusBadGateway
	if _, _, err := tr.Transcribe(ctx, []byte("a"), "audio/webm", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("502: %v", err)
	}
	status = http.StatusOK
	body = `not json`
	if _, _, err := tr.Transcribe(ctx, []byte("a"), "audio/webm", ""); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("bad json: %v", err)
	}
	var off *Transcriber
	if off.Enabled() {
		t.Fatal("nil transcriber must be disabled")
	}
	if _, _, err := NewTranscriber("x", "", "m").Transcribe(ctx, []byte("a"), "", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no key: %v", err)
	}
}

type whisperStub struct {
	mu       sync.Mutex
	detected string
	langs    map[string]string
	scores   map[string]float64
	nospeech float64
	calls    []string
	models   []string
}

func (w *whisperStub) handler(t *testing.T) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		if r.FormValue("prompt") != "" {
			t.Errorf("prompt must not be sent")
		}
		if r.FormValue("response_format") != "verbose_json" {
			t.Errorf("format = %q", r.FormValue("response_format"))
		}
		lang := r.FormValue("language")
		w.mu.Lock()
		w.calls = append(w.calls, lang)
		w.models = append(w.models, r.FormValue("model"))
		w.mu.Unlock()
		var key string
		name := w.detected
		if lang != "" {
			key = lang
			name = map[string]string{"hi": "hindi", "te": "telugu", "en": "english"}[lang]
		} else {
			key = map[string]string{"hindi": "hi", "telugu": "te", "english": "en", "urdu": "hi", "tamil": "te", "welsh": "en"}[w.detected]
		}
		text := w.langs[key]
		sc := w.scores[key]
		b := `{"text":"` + text + `","language":"` + name + `","segments":[{"start":0,"end":3,"avg_logprob":` + ftoa(sc) + `,"no_speech_prob":` + ftoa(w.nospeech) + `}]}`
		rw.Write([]byte(b))
	}
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}

func TestTranscribeFixesHindiMisheardAsTelugu(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "మైనే లంచ్ పర్ 250 ఖర్చ్", "hi": "मैंने लंच पर 250 खर्च किए"},
		scores:   map[string]float64{"te": -1.1, "hi": -0.25},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "hi" || !strings.Contains(text, "खर्च") {
		t.Fatalf("text=%q lang=%q err=%v calls=%v", text, lang, err, stub.calls)
	}
}

func TestTranscribeKeepsTeluguWhenItWins(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "నేను లంచ్ కోసం 250 ఖర్చు చేశాను", "hi": "नेनु लंच कोसम 250"},
		scores:   map[string]float64{"te": -0.3, "hi": -0.9},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	_, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "hi")
	if err != nil || lang != "te" {
		t.Fatalf("lang=%q err=%v", lang, err)
	}
}

func TestTranscribeHintBreaksNearTie(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "నేను 250 ఖర్చు", "hi": "मैंने 250 खर्च"},
		scores:   map[string]float64{"te": -0.5, "hi": -0.55},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	_, lang, _ := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if lang != "te" {
		t.Fatalf("no hint: %q", lang)
	}
	_, lang, _ = tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "hi")
	if lang != "te" && lang != "hi" {
		t.Fatalf("lang = %q", lang)
	}
}

func TestTranscribeUrduDetectionBecomesDevanagari(t *testing.T) {
	stub := &whisperStub{
		detected: "urdu",
		langs:    map[string]string{"hi": "मुझे कल याद दिलाओ", "te": "ముఝే కల్"},
		scores:   map[string]float64{"hi": -0.2, "te": -1.4},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "hi" || !strings.Contains(text, "याद") {
		t.Fatalf("text=%q lang=%q err=%v", text, lang, err)
	}
}

func TestTranscribeEnglishSingleCall(t *testing.T) {
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "I spent 250 rupees on lunch today with my friends"},
		scores:   map[string]float64{"en": -0.1},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "en")
	if err != nil || lang != "en" || text != "I spent 250 rupees on lunch today with my friends" || len(stub.calls) != 1 {
		t.Fatalf("text=%q lang=%q err=%v calls=%v", text, lang, err, stub.calls)
	}
}

func TestTranscribeUnknownLanguageForcesEnglish(t *testing.T) {
	stub := &whisperStub{
		detected: "welsh",
		langs:    map[string]string{"en": "add tea 20"},
		scores:   map[string]float64{"en": -0.3},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "en" || text != "add tea 20" {
		t.Fatalf("text=%q lang=%q err=%v calls=%v", text, lang, err, stub.calls)
	}
}

func TestTranscribeHighNoSpeechIsRejected(t *testing.T) {
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "thanks for the music"},
		scores:   map[string]float64{"en": -1.5},
		nospeech: 0.9,
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	if _, _, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); !errors.Is(err, ErrNoSpeech) {
		t.Fatalf("err = %v", err)
	}
}

func TestTranscribeShortEnglishIsReCheckedForIndic(t *testing.T) {
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "hello no", "hi": "हेलो नहीं", "te": "హలో నో"},
		scores:   map[string]float64{"en": -0.9, "hi": -0.8, "te": -0.2},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "te" || !strings.Contains(text, "హలో") {
		t.Fatalf("text=%q lang=%q err=%v calls=%v", text, lang, err, stub.calls)
	}
}

func TestTranscribeConfidentLongEnglishStaysEnglish(t *testing.T) {
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "I spent two hundred and fifty rupees on lunch today", "hi": "x", "te": "y"},
		scores:   map[string]float64{"en": -0.12, "hi": -0.1, "te": -0.1},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	_, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "en" || len(stub.calls) != 1 {
		t.Fatalf("lang=%q err=%v calls=%v", lang, err, stub.calls)
	}
}

func TestTranscribeShortEnglishStaysWhenIndicIsWorse(t *testing.T) {
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "yes", "hi": "यस", "te": "యెస్"},
		scores:   map[string]float64{"en": -0.2, "hi": -1.2, "te": -1.3},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	_, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "en" {
		t.Fatalf("lang=%q err=%v", lang, err)
	}
}

func TestForcedIndicPassesUseTheStrongerModel(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "నేను 250 ఖర్చు", "hi": "मैंने 250 खर्च"},
		scores:   map[string]float64{"te": -0.3, "hi": -1.0},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "turbo").WithIndicModel("large")
	if _, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); err != nil || lang != "te" {
		t.Fatalf("lang=%q err=%v", lang, err)
	}
	for i, c := range stub.calls {
		want := "large"
		if c == "" {
			want = "turbo"
		}
		if stub.models[i] != want {
			t.Fatalf("call %q used %q, want %q", c, stub.models[i], want)
		}
	}
	if len(stub.calls) != 3 {
		t.Fatalf("calls = %v", stub.calls)
	}
}

func TestSilenceNeedsLowConfidenceToo(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "నేను 250 ఖర్చు", "hi": "x"},
		scores:   map[string]float64{"te": -0.4, "hi": -1.5},
		nospeech: 0.7,
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	if _, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); err != nil || lang != "te" {
		t.Fatalf("confident speech with some no-speech probability must pass: lang=%q err=%v", lang, err)
	}
}

func TestIndicFlipNeedsBigEvidence(t *testing.T) {
	stub := &whisperStub{
		detected: "telugu",
		langs:    map[string]string{"te": "నేను 250 ఖర్చు", "hi": "मैंने 250 खर्च"},
		scores:   map[string]float64{"te": -0.6, "hi": -0.3},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()
	tr := NewTranscriber(srv.URL, "k", "m")
	if _, lang, _ := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); lang != "te" {
		t.Fatalf("a small lead must not flip a confident detection: %q", lang)
	}
	stub.scores = map[string]float64{"te": -1.3, "hi": -0.3}
	if _, lang, _ := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); lang != "hi" {
		t.Fatalf("a big lead should flip: %q", lang)
	}
}

func sarvamServer(t *testing.T, status int, body string, got *map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-subscription-key") != "sk" || r.URL.Path != "/speech-to-text" {
			t.Errorf("bad request: key=%q path=%q", r.Header.Get("api-subscription-key"), r.URL.Path)
		}
		r.ParseMultipartForm(1 << 20)
		if got != nil {
			*got = map[string]string{"language_code": r.FormValue("language_code"), "model": r.FormValue("model")}
			if _, h, err := r.FormFile("file"); err == nil {
				(*got)["ctype"] = h.Header.Get("Content-Type")
			}
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestSarvamTranscribesTeluguAndDetectsLanguage(t *testing.T) {
	var got map[string]string
	srv := sarvamServer(t, 200, `{"transcript":"నేను లంచ్ కోసం 250 ఖర్చు చేశాను","language_code":"te-IN"}`, &got)
	defer srv.Close()
	tr := NewTranscriber("http://127.0.0.1:1", "", "m")
	tr.Sarvam = NewSarvamSTT(srv.URL, "sk", "saaras:v4")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm;codecs=opus", "")
	if err != nil || lang != "te" || !strings.Contains(text, "250") {
		t.Fatalf("text=%q lang=%q err=%v", text, lang, err)
	}
	if got["language_code"] != "unknown" || got["model"] != "saaras:v4" || got["ctype"] != "audio/webm" {
		t.Fatalf("sent: %v", got)
	}
}

func TestSarvamLanguageCodesAndScriptWin(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"transcript":"I spent 100 on tea","language_code":"en-IN"}`, "en"},
		{`{"transcript":"मैंने चाय पर 50 खर्च किए","language_code":"hi-IN"}`, "hi"},
		{`{"transcript":"मुझे कल याद दिलाओ","language_code":"mr-IN"}`, "hi"},
		{`{"transcript":"నేను టీ కోసం 20","language_code":"hi-IN"}`, "te"},
		{`{"transcript":"spent 20 on tea","language_code":null}`, "en"},
	}
	for _, c := range cases {
		srv := sarvamServer(t, 200, c.body, nil)
		s := NewSarvamSTT(srv.URL, "sk", "")
		_, lang, err := s.Transcribe(context.Background(), []byte("a"), "audio/webm")
		srv.Close()
		if err != nil || lang != c.want {
			t.Errorf("%s -> %q err=%v, want %q", c.body, lang, err, c.want)
		}
	}
}

func TestSarvamEmptyIsNoSpeechWithoutWhisperFallback(t *testing.T) {
	srv := sarvamServer(t, 200, `{"transcript":"  ","language_code":"te-IN"}`, nil)
	defer srv.Close()
	whisperCalled := false
	w := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { whisperCalled = true }))
	defer w.Close()
	tr := NewTranscriber(w.URL, "k", "m")
	tr.Sarvam = NewSarvamSTT(srv.URL, "sk", "")
	if _, _, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); !errors.Is(err, ErrNoSpeech) || whisperCalled {
		t.Fatalf("err=%v whisperCalled=%v", err, whisperCalled)
	}
}

func TestSarvamFailureFallsBackToWhisper(t *testing.T) {
	srv := sarvamServer(t, 503, `down`, nil)
	defer srv.Close()
	stub := &whisperStub{
		detected: "english",
		langs:    map[string]string{"en": "I spent 250 rupees on lunch today with my friends"},
		scores:   map[string]float64{"en": -0.1},
	}
	w := httptest.NewServer(stub.handler(t))
	defer w.Close()
	tr := NewTranscriber(w.URL, "k", "m")
	tr.Sarvam = NewSarvamSTT(srv.URL, "sk", "")
	text, lang, err := tr.Transcribe(context.Background(), []byte("a"), "audio/webm", "")
	if err != nil || lang != "en" || !strings.Contains(text, "250") {
		t.Fatalf("text=%q lang=%q err=%v", text, lang, err)
	}
	tr2 := NewTranscriber("http://127.0.0.1:1", "", "")
	tr2.Sarvam = NewSarvamSTT(srv.URL, "sk", "")
	if !tr2.Enabled() {
		t.Fatal("sarvam alone should enable transcription")
	}
	if _, _, err := tr2.Transcribe(context.Background(), []byte("a"), "audio/webm", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no whisper to fall back to: %v", err)
	}
}
