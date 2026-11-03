# TellR

A voice assistant for money and reminders. Speak a sentence, the AI works out what you mean, TellR changes your data, and it answers out loud.

Built solo for the Vocal Labs Round 2 hackathon. Built with AI assistance (Claude), reviewed and run by me.

## What it does

- Say "spent 250 on lunch and remind me to call mom tomorrow at 6" and both are saved.
- Ask "how much did I spend this month" and get a spoken summary with a comparison to the last period.
- Set monthly budgets by category, with spoken warnings at 80% and 100%. Add your own budget names (like "gym" or "trip"), rename one by voice, and use the name as an expense category.
- Summaries for today, the last 7 days, this month, or any week of the month (week 1 to 4).
- Import past expenses from a CSV file (`samples/demo-expenses.csv` shows the format). Duplicates and bad rows are skipped and reported.
- Repeating reminders (daily, weekly, monthly) that advance when marked done.
- Change an entry by voice ("make the last expense 300 and transport").
- Delete by voice, but only after a spoken yes or no. A delete never runs without confirmation.
- Search expenses, export CSV, daily trend chart.
- Talk about everyday things too: greetings, time and date, general questions.
- English, Hindi and Telugu. Auto mode detects the language you speak and replies in it. A pause of up to about two seconds does not end your turn.

## How it works

1. The browser records speech (Web Speech API in a fixed language, or a recording sent to the server in Auto mode). The server transcribes it with Sarvam Saaras when a Sarvam key is set, and falls back to Groq Whisper otherwise.
2. The Go server sends the text to the Groq LLM, which returns a JSON list of actions.
3. The server validates every action itself (amounts, ids, dates, categories) and runs them in one database transaction.
4. The server builds the reply, translates it for Hindi or Telugu, and the browser speaks it.

The LLM only proposes actions. It never touches the database directly.

## Run it

Needs Go 1.26 or newer.

```
export GROQ_API_KEY=your_key_here
export SARVAM_API_KEY=your_key_here
go run .
```

Open http://localhost:8080 in Chrome. Without a Groq key, the forms still work and voice commands are off. Without a Sarvam key, Telugu and Hindi still work through Groq Whisper and device voices, with lower quality.

| Variable | Default | Purpose |
|---|---|---|
| GROQ_API_KEY | none | Groq key, read from the environment only |
| SARVAM_API_KEY | none | Sarvam key for Hindi and Telugu recognition and voice, read from the environment only |
| SARVAM_STT_MODEL | saaras:v4 | Sarvam speech recognition model |
| SARVAM_MODEL | bulbul:v3 | Sarvam voice model |
| SARVAM_SPEAKER | priya | Sarvam voice |
| GROQ_STT_INDIC_MODEL | whisper-large-v3 | Whisper model used for Hindi and Telugu fallback |
| PORT | 8080 | HTTP port |
| DB_PATH | data/tellr.db | SQLite file |
| GROQ_MODEL | openai/gpt-oss-120b | Planner model |
| GROQ_STT_MODEL | whisper-large-v3-turbo | Speech recognition |
| GROQ_TTS_MODEL | canopylabs/orpheus-v1-english | Natural English voice |
| TRUSTED_PROXY | false | Set true behind Render or another proxy |

Run the tests with `go test ./...`.

## Project layout

- `models` data types and validation
- `store` SQLite storage (pure Go driver, no cgo)
- `actions` validates and runs the AI's actions
- `ai` Groq and Sarvam clients, prompt, speech recognition, Indian-language voice, small talk
- `confirm` single-use delete confirmations
- `handlers` HTTP API
- `web` the single-page frontend, embedded into the binary

## What is real and what is not

Real:
- Voice input, the AI planning, database writes, spoken replies and in-page reminder alerts all work end to end.
- Hindi and Telugu input and replies, with natural Sarvam voices when a Sarvam key is set.
- CSV import of past expenses.

Limits, stated honestly:
- There is no login. Anyone with the URL shares one dataset. This is a demo.
- Reminders alert inside the open page. There is no email, SMS or WhatsApp.
- The natural English voice (Orpheus) needs its terms accepted once in the Groq console. Hindi and Telugu voices come from Sarvam and need a Sarvam key. Without these, the app uses your device's voices, whose quality depends on the voices installed.
- Voice quality and recognition depend on third-party services (Groq, Sarvam) and their free-tier limits.
- Hindi and Telugu replies are machine-translated from English.
- Auto language detection can still misjudge very short phrases. Pick the language manually if it does.
- On a free hosting plan the app sleeps when idle and may lose its data on restart.
- Live data (weather, news) is not available to the assistant.
