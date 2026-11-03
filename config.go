package main

import (
	"strconv"
	"strings"
)

type config struct {
	Port        string
	DBPath      string
	APIKey      string
	Model       string
	BaseURL     string
	TTSModel    string
	TTSVoice    string
	STTModel    string
	TrustProxy  bool
	VoicePerMin int
	IPPerMin    int
}

func loadConfig(get func(string) string) config {
	c := config{
		Port:        orDefault(get("PORT"), "8080"),
		DBPath:      orDefault(get("DB_PATH"), "data/tellr.db"),
		APIKey:      strings.TrimSpace(get("GROQ_API_KEY")),
		Model:       orDefault(get("GROQ_MODEL"), "openai/gpt-oss-120b"),
		BaseURL:     orDefault(get("GROQ_BASE_URL"), "https://api.groq.com/openai/v1"),
		TTSModel:    orDefault(get("GROQ_TTS_MODEL"), "canopylabs/orpheus-v1-english"),
		TTSVoice:    orDefault(get("GROQ_TTS_VOICE"), "hannah"),
		STTModel:    orDefault(get("GROQ_STT_MODEL"), "whisper-large-v3-turbo"),
		TrustProxy:  strings.EqualFold(strings.TrimSpace(get("TRUSTED_PROXY")), "true"),
		VoicePerMin: positiveInt(get("VOICE_PER_MIN"), 30),
		IPPerMin:    positiveInt(get("IP_PER_MIN"), 12),
	}
	return c
}

func orDefault(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func positiveInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}
