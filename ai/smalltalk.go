package ai

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type talkKind int

const (
	talkNone talkKind = iota
	talkGreeting
	talkMorning
	talkAfternoon
	talkEvening
	talkNight
	talkHowAreYou
	talkThanks
	talkBye
	talkName
	talkCapabilities
	talkTime
	talkDate
	talkDay
	talkMaker
	talkSorry
	talkAck
	talkLaugh
)

var talkPhrases = map[string]talkKind{
	"hi": talkGreeting, "hii": talkGreeting, "hello": talkGreeting, "hey": talkGreeting, "hey there": talkGreeting,
	"hi there": talkGreeting, "hello there": talkGreeting, "hey hey": talkGreeting, "yo": talkGreeting, "hola": talkGreeting,
	"namaste": talkGreeting, "namaskar": talkGreeting, "namaskaram": talkGreeting, "vanakkam": talkGreeting,
	"नमस्ते": talkGreeting, "नमस्कार": talkGreeting, "हेलो": talkGreeting, "हैलो": talkGreeting, "हाय": talkGreeting,
	"హాయ్": talkGreeting, "హలో": talkGreeting, "నమస్కారం": talkGreeting, "నమస్తే": talkGreeting,
	"good morning": talkMorning, "सुप्रभात": talkMorning, "శుభోదయం": talkMorning,
	"good afternoon": talkAfternoon, "good evening": talkEvening, "good night": talkNight, "शुभ रात्रि": talkNight, "శుభ రాత్రి": talkNight,

	"how are you": talkHowAreYou, "how are you doing": talkHowAreYou, "how do you do": talkHowAreYou,
	"how is it going": talkHowAreYou, "hows it going": talkHowAreYou, "whats up": talkHowAreYou, "what is up": talkHowAreYou,
	"sup": talkHowAreYou, "how have you been": talkHowAreYou, "are you fine": talkHowAreYou, "are you okay": talkHowAreYou,
	"how are you today": talkHowAreYou, "hello how are you": talkHowAreYou, "hi how are you": talkHowAreYou,
	"आप कैसे हैं": talkHowAreYou, "कैसे हो": talkHowAreYou, "तुम कैसे हो": talkHowAreYou, "कैसी हो": talkHowAreYou,
	"ఎలా ఉన్నావు": talkHowAreYou, "మీరు ఎలా ఉన్నారు": talkHowAreYou, "ఎలా ఉన్నారు": talkHowAreYou,

	"thanks": talkThanks, "thank you": talkThanks, "thank you so much": talkThanks, "thanks a lot": talkThanks,
	"thanks so much": talkThanks, "thank you very much": talkThanks, "many thanks": talkThanks, "thx": talkThanks,
	"thanks tellr": talkThanks, "thank u": talkThanks, "cheers": talkThanks, "much appreciated": talkThanks,
	"धन्यवाद": talkThanks, "शुक्रिया": talkThanks, "थैंक यू": talkThanks, "ధన్యవాదాలు": talkThanks, "థాంక్స్": talkThanks, "థాంక్ యు": talkThanks,

	"bye": talkBye, "goodbye": talkBye, "good bye": talkBye, "bye bye": talkBye, "see you": talkBye, "see you later": talkBye,
	"see you soon": talkBye, "talk to you later": talkBye, "catch you later": talkBye, "take care": talkBye, "ttyl": talkBye,
	"अलविदा": talkBye, "बाय": talkBye, "फिर मिलेंगे": talkBye, "బై": talkBye, "వెళ్తాను": talkBye,

	"what is your name": talkName, "whats your name": talkName, "who are you": talkName, "your name": talkName,
	"tell me your name": talkName, "may i know your name": talkName, "what should i call you": talkName,
	"what do i call you": talkName, "introduce yourself": talkName, "tell me about yourself": talkName,
	"what are you": talkName, "who am i talking to": talkName,
	"तुम्हारा नाम क्या है": talkName, "आपका नाम क्या है": talkName, "तुम कौन हो": talkName, "आप कौन हैं": talkName,
	"నీ పేరు ఏమిటి": talkName, "మీ పేరు ఏమిటి": talkName, "నువ్వు ఎవరు": talkName, "మీరు ఎవరు": talkName,

	"what can you do": talkCapabilities, "what do you do": talkCapabilities, "what is your work": talkCapabilities,
	"whats your work": talkCapabilities, "what is your job": talkCapabilities, "how can you help me": talkCapabilities,
	"how can you help": talkCapabilities, "can you help me": talkCapabilities, "help": talkCapabilities, "help me": talkCapabilities,
	"what are your features": talkCapabilities, "what are your skills": talkCapabilities, "how do you work": talkCapabilities,
	"how do i use you": talkCapabilities, "what can i ask you": talkCapabilities, "what can i say": talkCapabilities,
	"आप क्या कर सकते हैं": talkCapabilities, "तुम क्या कर सकते हो": talkCapabilities, "तुम्हारा काम क्या है": talkCapabilities,
	"నువ్వు ఏమి చేయగలవు": talkCapabilities, "మీరు ఏమి చేయగలరు": talkCapabilities, "నీ పని ఏమిటి": talkCapabilities,

	"what time is it": talkTime, "what is the time": talkTime, "whats the time": talkTime, "current time": talkTime,
	"time now": talkTime, "tell me the time": talkTime, "what is the time now": talkTime, "what is the current time": talkTime,
	"whats the time now": talkTime, "time please": talkTime, "can you tell me the time": talkTime, "do you know the time": talkTime,
	"what time is it now": talkTime, "the time": talkTime, "time": talkTime,
	"अभी क्या समय हुआ है": talkTime, "समय क्या हुआ है": talkTime, "टाइम क्या हुआ है": talkTime, "अभी कितने बजे हैं": talkTime, "कितने बजे हैं": talkTime,
	"ఇప్పుడు టైం ఎంత": talkTime, "టైం ఎంత": talkTime, "ఇప్పుడు సమయం ఎంత": talkTime, "సమయం ఎంత": talkTime,

	"what is the date": talkDate, "whats the date": talkDate, "what is the date today": talkDate, "whats the date today": talkDate,
	"todays date": talkDate, "what is todays date": talkDate, "whats todays date": talkDate, "what date is it": talkDate,
	"what date is today": talkDate, "tell me the date": talkDate, "current date": talkDate, "date today": talkDate, "date": talkDate,
	"आज की तारीख क्या है": talkDate, "आज कौन सी तारीख है": talkDate, "ఈ రోజు తేదీ ఎంత": talkDate, "ఈరోజు తేదీ ఎంత": talkDate,

	"what day is it": talkDay, "what day is today": talkDay, "which day is today": talkDay, "what day is it today": talkDay,
	"what is today": talkDay, "whats today": talkDay, "which day is it": talkDay, "today is which day": talkDay, "day today": talkDay,
	"आज कौन सा दिन है": talkDay, "आज कौन सा वार है": talkDay, "ఈ రోజు ఏ రోజు": talkDay, "ఈరోజు ఏ రోజు": talkDay,

	"who made you": talkMaker, "who created you": talkMaker, "who built you": talkMaker, "who developed you": talkMaker,
	"who designed you": talkMaker, "who is your creator": talkMaker, "who is your developer": talkMaker, "who programmed you": talkMaker,
	"तुम्हें किसने बनाया": talkMaker, "आपको किसने बनाया": talkMaker, "నిన్ను ఎవరు తయారు చేశారు": talkMaker, "నిన్ను ఎవరు సృష్టించారు": talkMaker,

	"sorry": talkSorry, "im sorry": talkSorry, "my bad": talkSorry, "oops": talkSorry, "excuse me": talkSorry, "pardon": talkSorry,
	"माफ़ करना": talkSorry, "सॉरी": talkSorry, "క్షమించు": talkSorry, "సారీ": talkSorry,

	"ok": talkAck, "okay": talkAck, "cool": talkAck, "nice": talkAck, "great": talkAck, "awesome": talkAck, "good": talkAck,
	"alright": talkAck, "fine": talkAck, "got it": talkAck, "i see": talkAck, "perfect": talkAck, "wow": talkAck, "amazing": talkAck,
	"good job": talkAck, "well done": talkAck, "nice work": talkAck, "great job": talkAck, "you are great": talkAck, "you are awesome": talkAck,
	"अच्छा": talkAck, "ठीक है": talkAck, "बढ़िया": talkAck, "ओके": talkAck, "సరే": talkAck, "బాగుంది": talkAck, "ఓకే": talkAck, "మంచిది": talkAck,

	"haha": talkLaugh, "lol": talkLaugh, "hahaha": talkLaugh, "funny": talkLaugh, "हाहा": talkLaugh, "హహ": talkLaugh,
}

var nameIntro = regexp.MustCompile(`^(?:my name is|my names|call me|i am called|im called|this is|मेरा नाम|నా పేరు)\s+([\p{L}\p{M}]+(?: [\p{L}\p{M}]+)?)(?: and what is your name| and whats your name)?$`)

var assistantNames = map[string]bool{"tellr": true, "teller": true, "telar": true, "teler": true, "teller's": true}

func normalizeTalk(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’' || r == '`':
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	out := words[:0]
	for _, w := range words {
		if !assistantNames[w] {
			out = append(out, w)
		}
	}
	words = out
	if len(words) > 1 {
		switch words[0] {
		case "hey", "ok", "okay", "oh", "so", "um", "well":
			if _, ok := talkPhrases[strings.Join(words, " ")]; !ok {
				words = words[1:]
			}
		}
	}
	return strings.Join(words, " ")
}

func titleName(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func SmallTalk(text string, now time.Time) (string, bool) {
	norm := normalizeTalk(text)
	if norm == "" || len([]rune(norm)) > 60 {
		return "", false
	}
	if m := nameIntro.FindStringSubmatch(norm); m != nil {
		name := titleName(m[1])
		if len([]rune(name)) <= 30 {
			return fmt.Sprintf("Nice to meet you, %s! I'm TellR. I track your spending and reminders, and I'm happy to chat too.", name), true
		}
	}
	kind, ok := talkPhrases[norm]
	if !ok {
		return "", false
	}
	switch kind {
	case talkGreeting:
		return greetingFor(now), true
	case talkMorning:
		return "Good morning! Ready when you are. Tell me an expense, a reminder, or just ask me something.", true
	case talkAfternoon:
		return "Good afternoon! How can I help you today?", true
	case talkEvening:
		return "Good evening! How was your day? Tell me anything you spent or need to remember.", true
	case talkNight:
		return "Good night! Sleep well. I'll keep your reminders safe.", true
	case talkHowAreYou:
		return "I'm doing great, thanks for asking! How are you? Tell me what you spent today, or ask me anything.", true
	case talkThanks:
		return "You're welcome! I'm here whenever you need me.", true
	case talkBye:
		return "Goodbye! Talk to you soon.", true
	case talkName:
		return "I'm TellR, your voice assistant. I track your expenses and reminders, and I can chat and answer everyday questions too.", true
	case talkCapabilities:
		return "I track what you spend, set reminders, keep monthly budgets and show summaries. Just say it, like \"spent 250 on lunch\" or \"remind me to call mom at 6\". I can chat and answer everyday questions too.", true
	case talkTime:
		return "It's " + now.Format("3:04 PM") + ".", true
	case talkDate:
		return "Today is " + now.Format("Monday, 2 January 2006") + ".", true
	case talkDay:
		return "Today is " + now.Format("Monday") + ".", true
	case talkMaker:
		return "I was built by Ranjith Kumar as a voice assistant for expenses and reminders.", true
	case talkSorry:
		return "No problem at all!", true
	case talkAck:
		return "Great! Let me know if you need anything else.", true
	case talkLaugh:
		return "Glad that made you smile! Anything else I can do for you?", true
	}
	return "", false
}

func greetingFor(now time.Time) string {
	switch h := now.Hour(); {
	case h < 12:
		return "Hello, good morning! I'm TellR. Tell me an expense or a reminder, or just chat with me."
	case h < 17:
		return "Hello, good afternoon! I'm TellR. Tell me an expense or a reminder, or just chat with me."
	default:
		return "Hello, good evening! I'm TellR. Tell me an expense or a reminder, or just chat with me."
	}
}
