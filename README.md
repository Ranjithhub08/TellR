# TellR

A voice assistant for money and reminders. Speak a sentence, the AI works out what you mean, TellR changes your data, and it answers out loud.

Built solo for the Vocal Labs Round 2 hackathon. Built with AI assistance (Claude), reviewed and run by me.

## What it does

- Say "spent 250 on lunch and remind me to call mom tomorrow at 6" and both are saved.
- Ask "how much did I spend this month" and get a spoken summary with a comparison to the last period.
- Set monthly budgets by category, with spoken warnings at 80% and 100%.
- Repeating reminders (daily, weekly, monthly) that advance when marked done.
- Change an entry by voice ("make the last expense 300 and transport").
- Delete by voice, but only after a spoken yes or no. A delete never runs without confirmation.
- Search expenses, export CSV, daily trend chart.
- Talk about everyday things too: greetings, time and date, general questions.
- English, Hindi and Telugu. Auto mode detects the language you speak and replies in it.

## How it works

1. The browser records speech (Web Speech API, or Groq Whisper in Auto mode).
2. The Go server sends the text to the Groq LLM, which returns a JSON list of actions.
3. The server validates every action itself (amounts, ids, dates, categories) and runs them in one database transaction.
4. The server builds the reply, translates it for Hindi or Telugu, and the browser speaks it.

The LLM only proposes actions. It never touches the database directly.

## Run it

Needs Go 1.26 or newer.

```
export GROQ_API_KEY=your_key_here
go run .
```

Open http://localhost:8080 in Chrome. Without a key, the forms still work and voice commands are off.

| Variable | Default | Purpose |
|---|---|---|
| GROQ_API_KEY | none | Groq key, read from the environment only |
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
- `ai` Groq client, prompt, speech recognition, small talk
- `confirm` single-use delete confirmations
- `handlers` HTTP API
- `web` the single-page frontend, embedded into the binary

## What is real and what is not

Real:
- Voice input, the AI planning, database writes, spoken replies and in-page reminder alerts all work end to end.
- Hindi and Telugu input and replies.

Limits, stated honestly:
- There is no login. Anyone with the URL shares one dataset. This is a demo.
- Reminders alert inside the open page. There is no email, SMS or WhatsApp.
- The natural voice (Orpheus) speaks English only, and needs its terms accepted once in the Groq console. Hindi and Telugu replies use your device's voices, whose quality depends on the voices installed.
- Hindi and Telugu replies are machine-translated from English.
- Auto language detection can still misjudge very short phrases. Pick the language manually if it does.
- On a free hosting plan the app sleeps when idle and may lose its data on restart.
- Live data (weather, news) is not available to the assistant.
