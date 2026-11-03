package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

var ErrNoSpeech = errors.New("no speech was found in the recording")

type Transcriber struct {
	BaseURL string
	APIKey  string
	Model   string
	Indic   string
	Sarvam  *SarvamSTT
	Log     *slog.Logger
	HTTP    *http.Client
}

func NewTranscriber(baseURL, apiKey, model string) *Transcriber {
	return &Transcriber{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *Transcriber) WithIndicModel(m string) *Transcriber {
	t.Indic = strings.TrimSpace(m)
	return t
}

func (t *Transcriber) modelFor(lang string) string {
	if (lang == "hi" || lang == "te") && t.Indic != "" {
		return t.Indic
	}
	return t.Model
}

func (t *Transcriber) whisperOK() bool {
	return t != nil && t.APIKey != "" && t.Model != ""
}

func (t *Transcriber) Enabled() bool {
	return t.whisperOK() || (t != nil && t.Sarvam.Enabled())
}

func audioExt(mime string) string {
	m := strings.ToLower(mime)
	switch {
	case strings.Contains(m, "webm"):
		return "webm"
	case strings.Contains(m, "ogg"):
		return "ogg"
	case strings.Contains(m, "mp4"), strings.Contains(m, "m4a"), strings.Contains(m, "aac"):
		return "m4a"
	case strings.Contains(m, "wav"):
		return "wav"
	case strings.Contains(m, "mpeg"), strings.Contains(m, "mp3"):
		return "mp3"
	}
	return "webm"
}

type whisperSegment struct {
	Start        float64 `json:"start"`
	End          float64 `json:"end"`
	AvgLogprob   float64 `json:"avg_logprob"`
	NoSpeechProb float64 `json:"no_speech_prob"`
}

type whisperResult struct {
	Text     string           `json:"text"`
	Language string           `json:"language"`
	Segments []whisperSegment `json:"segments"`
}

func (r whisperResult) score() float64 {
	var sum, dur float64
	for _, s := range r.Segments {
		d := s.End - s.Start
		if d <= 0 {
			d = 0.1
		}
		sum += s.AvgLogprob * d
		dur += d
	}
	if dur == 0 {
		return -5
	}
	return sum / dur
}

func (r whisperResult) noSpeech() float64 {
	var sum, dur float64
	for _, s := range r.Segments {
		d := s.End - s.Start
		if d <= 0 {
			d = 0.1
		}
		sum += s.NoSpeechProb * d
		dur += d
	}
	if dur == 0 {
		return 0
	}
	return sum / dur
}

func langFamily(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "english", "en":
		return "en"
	case "hindi", "hi", "urdu", "ur", "marathi", "mr", "nepali", "ne", "punjabi", "pa", "gujarati", "gu", "bengali", "bn", "sanskrit", "sa", "assamese", "as", "sindhi", "sd":
		return "hi"
	case "telugu", "te", "tamil", "ta", "kannada", "kn", "malayalam", "ml":
		return "te"
	}
	return ""
}

const (
	flipMargin = 0.15
	indicFlip  = 0.4
	hintBonus  = 0.1
	badScript  = 1.0
)

func (t *Transcriber) Transcribe(ctx context.Context, audio []byte, mime, hint string) (string, string, error) {
	if !t.Enabled() {
		return "", "", ErrNotConfigured
	}
	if t.Sarvam.Enabled() {
		text, lang, err := t.Sarvam.Transcribe(ctx, audio, mime)
		if err == nil {
			if t.Log != nil {
				t.Log.Info("sarvam transcription", "lang", lang)
			}
			return text, lang, nil
		}
		if errors.Is(err, ErrNoSpeech) || !t.whisperOK() {
			return "", "", err
		}
		if t.Log != nil {
			t.Log.Warn("sarvam transcription failed, using whisper", "err", err)
		}
	}
	first, err := t.call(ctx, audio, mime, "")
	if err != nil {
		return "", "", err
	}
	if first.noSpeech() > 0.6 && first.score() < -1.0 {
		return "", "", ErrNoSpeech
	}
	fam := langFamily(first.Language)
	switch fam {
	case "hi", "te":
		return t.pickIndic(ctx, audio, mime, hint, first, fam, []string{"hi", "te"})
	case "en":
		if mustVerifyEnglish(first, hint) {
			return t.pickIndic(ctx, audio, mime, hint, first, "en", []string{"en", "hi", "te"})
		}
		return finish(first.Text, "en")
	}
	forced, err := t.call(ctx, audio, mime, "en")
	if err != nil {
		return "", "", err
	}
	return finish(forced.Text, "en")
}

func mustVerifyEnglish(first whisperResult, hint string) bool {
	if hint == "hi" || hint == "te" {
		return true
	}
	if first.score() < -0.45 {
		return true
	}
	return len(strings.Fields(first.Text)) <= 5
}

func finish(text, lang string) (string, string, error) {
	text = SanitizeSpeech(text)
	if text == "" || isFiller(text) {
		return "", "", ErrNoSpeech
	}
	if lang != "en" {
		if got := DetectLang(text); got != lang {
			lang = got
		}
	}
	return text, lang, nil
}

func (t *Transcriber) pickIndic(ctx context.Context, audio []byte, mime, hint string, first whisperResult, fam string, langs []string) (string, string, error) {
	cands := map[string]whisperResult{}
	reuse := fam == "en" || t.Indic == "" || t.Indic == t.Model
	switch strings.ToLower(first.Language) {
	case "hindi", "telugu", "english":
		if reuse {
			cands[fam] = first
		}
	}
	var need []string
	for _, l := range langs {
		if _, ok := cands[l]; !ok {
			need = append(need, l)
		}
	}
	results := make([]whisperResult, len(need))
	errs := make([]error, len(need))
	var wg sync.WaitGroup
	for i, l := range need {
		wg.Add(1)
		go func(i int, l string) {
			defer wg.Done()
			results[i], errs[i] = t.call(ctx, audio, mime, l)
		}(i, l)
	}
	wg.Wait()
	var firstErr error
	for i, l := range need {
		if errs[i] != nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		cands[l] = results[i]
	}
	if len(cands) == 0 {
		return "", "", firstErr
	}
	scores := map[string]float64{}
	for l, r := range cands {
		sc := r.score()
		if DetectLang(SanitizeSpeech(r.Text)) != l {
			sc -= badScript
		}
		if l == hint {
			sc += hintBonus
		}
		scores[l] = sc
	}
	best := ""
	for _, l := range append([]string{fam}, langs...) {
		if _, ok := cands[l]; ok {
			best = l
			break
		}
	}
	top := best
	for _, l := range langs {
		if _, ok := cands[l]; ok && scores[l] > scores[top] {
			top = l
		}
	}
	margin := flipMargin
	if best != "en" && top != "en" {
		margin = indicFlip
	}
	if top != best && scores[top] > scores[best]+margin {
		best = top
	}
	if t.Log != nil {
		t.Log.Info("language pick", "detected", first.Language, "hint", hint, "te", scores["te"], "hi", scores["hi"], "en", scores["en"], "chosen", best)
	}
	return finish(cands[best].Text, best)
}

func (t *Transcriber) call(ctx context.Context, audio []byte, mime, language string) (whisperResult, error) {
	var out whisperResult
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fields := map[string]string{
		"model":           t.modelFor(language),
		"response_format": "verbose_json",
		"temperature":     "0",
	}
	if language != "" {
		fields["language"] = language
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return out, err
		}
	}
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="speech.%s"`, audioExt(mime)))
	hdr.Set("Content-Type", "application/octet-stream")
	part, err := w.CreatePart(hdr)
	if err != nil {
		return out, err
	}
	if _, err := part.Write(audio); err != nil {
		return out, err
	}
	if err := w.Close(); err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return out, ErrBusy
	case resp.StatusCode >= 500:
		return out, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return out, fmt.Errorf("ai: transcription returned %d: %s", resp.StatusCode, snippet(data))
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, ErrBadOutput
	}
	return out, nil
}

func isFiller(s string) bool {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), ".!? ,"))
	switch s {
	case "you", "thank you", "thanks for watching", "thanks for watching!", "bye", "uh", "um", "hmm", "mm", "okay", "ok", "so", "the", "yeah":
		return true
	}
	return false
}
