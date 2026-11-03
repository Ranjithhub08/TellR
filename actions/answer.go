package actions

import "strings"

var yesWords = map[string]bool{
	"yes": true, "yeah": true, "yep": true, "yup": true, "sure": true, "ok": true, "okay": true,
	"confirm": true, "confirmed": true, "correct": true, "proceed": true,
	"haan": true, "han": true, "ha": true, "haa": true, "avunu": true, "avnu": true,
	"हाँ": true, "हां": true, "जी": true, "हाँ जी": true, "हां जी": true, "ठीक है": true, "जी हाँ": true, "जी हां": true,
	"అవును": true, "ఔను": true, "సరే": true,
	"do it": true, "go ahead": true, "delete it": true, "yes delete": true, "yes please": true,
	"yes do it": true, "yes delete it": true, "please do": true, "of course": true,
}

var noWords = map[string]bool{
	"no": true, "nope": true, "nah": true, "cancel": true, "stop": true, "dont": true,
	"nahi": true, "nahin": true, "na": true, "vaddu": true, "kadu": true,
	"नहीं": true, "नही": true, "मत करो": true, "रहने दो": true, "रुको": true, "కాదు": true, "వద్దు": true, "లేదు": true,
	"don't": true, "do not": true, "never mind": true, "nevermind": true, "leave it": true,
	"no thanks": true, "no cancel": true, "keep it": true, "dont delete": true, "don't delete": true,
}

var yesTokens = map[string]bool{
	"yes": true, "yeah": true, "yep": true, "yup": true, "sure": true, "ok": true, "okay": true,
	"confirm": true, "confirmed": true, "proceed": true, "delete": true, "haan": true, "avunu": true,
	"हाँ": true, "हां": true, "जी": true, "ठीक": true, "सही": true, "हटाओ": true, "डिलीट": true, "मिटाओ": true, "हटा": true,
	"అవును": true, "ఔను": true, "సరే": true, "తొలగించు": true, "డిలీట్": true,
}

var noTokens = map[string]bool{
	"no": true, "nope": true, "nah": true, "cancel": true, "stop": true, "dont": true, "don't": true,
	"not": true, "never": true, "nevermind": true, "keep": true, "nahi": true, "nahin": true,
	"vaddu": true, "kadu": true, "wait": true,
	"नहीं": true, "नही": true, "मत": true, "रुको": true, "रुकिए": true, "रहने": true, "कैंसल": true,
	"వద్దు": true, "కాదు": true, "లేదు": true, "ఆపు": true, "క్యాన్సిల్": true,
}

const maxAnswerWords = 5

func normalise(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.Map(func(r rune) rune {
		switch r {
		case '.', ',', '!', '?', ';', ':', '"', '।':
			return -1
		}
		return r
	}, t)
	return strings.Join(strings.Fields(t), " ")
}

func Answer(text string) (yes, no bool) {
	t := normalise(text)
	if t == "" {
		return false, false
	}
	if yesWords[t] {
		return true, false
	}
	if noWords[t] {
		return false, true
	}
	words := strings.Fields(t)
	if len(words) > maxAnswerWords {
		return false, false
	}
	hasYes, hasNo, literalYes, literalNo := false, false, false, false
	for _, w := range words {
		if w == "yes" {
			literalYes = true
		}
		if w == "no" {
			literalNo = true
		}
		if noTokens[w] {
			hasNo = true
		}
		if yesTokens[w] {
			hasYes = true
		}
	}
	if literalYes && literalNo {
		return false, false
	}
	if hasNo {
		return false, true
	}
	if hasYes {
		return true, false
	}
	return false, false
}
