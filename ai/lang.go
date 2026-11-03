package ai

import "unicode"

func DetectLang(text string) string {
	var te, hi int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Telugu, r):
			te++
		case unicode.Is(unicode.Devanagari, r):
			hi++
		}
	}
	switch {
	case te > 0 && te >= hi:
		return "te"
	case hi > 0:
		return "hi"
	}
	return "en"
}
