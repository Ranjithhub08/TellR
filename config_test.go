package main

import "testing"

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaults(t *testing.T) {
	c := loadConfig(envOf(nil))
	if c.Port != "8080" || c.DBPath != "data/tellr.db" || c.Model != "openai/gpt-oss-120b" ||
		c.BaseURL != "https://api.groq.com/openai/v1" || c.APIKey != "" || c.TrustProxy ||
		c.VoicePerMin != 30 || c.IPPerMin != 12 ||
		c.TTSModel != "canopylabs/orpheus-v1-english" || c.TTSVoice != "hannah" || c.STTModel != "whisper-large-v3-turbo" ||
		c.STTIndic != "whisper-large-v3" || c.SarvamKey != "" || c.SarvamSTT != "saaras:v4" || c.SarvamModel != "bulbul:v3" || c.SarvamVoice != "priya" {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	c := loadConfig(envOf(map[string]string{
		"PORT": " 10000 ", "DB_PATH": "/tmp/x.db", "GROQ_API_KEY": "  key  ", "GROQ_MODEL": "m",
		"GROQ_BASE_URL": "http://x/v1", "TRUSTED_PROXY": "TRUE", "VOICE_PER_MIN": "50", "IP_PER_MIN": "5", "SARVAM_API_KEY": " sk ", "SARVAM_SPEAKER": "kavya",
	}))
	if c.Port != "10000" || c.DBPath != "/tmp/x.db" || c.APIKey != "key" || c.Model != "m" ||
		c.BaseURL != "http://x/v1" || !c.TrustProxy || c.VoicePerMin != 50 || c.IPPerMin != 5 || c.SarvamKey != "sk" || c.SarvamVoice != "kavya" {
		t.Fatalf("env: %+v", c)
	}
}

func TestConfigRejectsBadNumbers(t *testing.T) {
	c := loadConfig(envOf(map[string]string{"VOICE_PER_MIN": "-4", "IP_PER_MIN": "abc"}))
	if c.VoicePerMin != 30 || c.IPPerMin != 12 {
		t.Fatalf("bad numbers accepted: %+v", c)
	}
}
