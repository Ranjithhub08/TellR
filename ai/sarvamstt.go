package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

type SarvamSTT struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func NewSarvamSTT(baseURL, apiKey, model string) *SarvamSTT {
	return &SarvamSTT{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *SarvamSTT) Enabled() bool {
	return s != nil && s.APIKey != "" && s.BaseURL != ""
}

func sarvamFamily(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "en-in", "en", "en-us", "en-gb":
		return "en"
	case "te-in", "te", "ta-in", "kn-in", "ml-in":
		return "te"
	case "hi-in", "hi", "mr-in", "ne-in", "pa-in", "gu-in", "bn-in", "ur-in", "kok-in", "mai-in", "doi-in", "sa-in":
		return "hi"
	}
	return ""
}

func cleanMime(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	if m == "" {
		return "audio/webm"
	}
	return m
}

func (s *SarvamSTT) Transcribe(ctx context.Context, audio []byte, mime string) (string, string, error) {
	if !s.Enabled() {
		return "", "", ErrNotConfigured
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fields := map[string]string{"language_code": "unknown"}
	if s.Model != "" {
		fields["model"] = s.Model
	}
	if s.Model == "saaras:v3" {
		fields["mode"] = "transcribe"
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return "", "", err
		}
	}
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="speech.%s"`, audioExt(mime)))
	hdr.Set("Content-Type", cleanMime(mime))
	part, err := w.CreatePart(hdr)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", "", err
	}
	if err := w.Close(); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/speech-to-text", &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("api-subscription-key", s.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", "", ErrBusy
	case resp.StatusCode >= 500:
		return "", "", fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", "", fmt.Errorf("ai: sarvam transcription returned %d: %s", resp.StatusCode, snippet(data))
	}
	var out struct {
		Transcript string `json:"transcript"`
		Language   string `json:"language_code"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", "", ErrBadOutput
	}
	text := SanitizeSpeech(out.Transcript)
	if text == "" || isFiller(text) {
		return "", "", ErrNoSpeech
	}
	lang := sarvamFamily(out.Language)
	script := DetectLang(text)
	if lang == "" || (lang != "en" && script != lang) || (lang == "en" && script != "en") {
		lang = script
	}
	return text, lang, nil
}
