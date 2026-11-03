CREATE TABLE IF NOT EXISTS expenses (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	amount_paise INTEGER NOT NULL CHECK (amount_paise > 0),
	category     TEXT NOT NULL,
	note         TEXT NOT NULL DEFAULT '',
	spent_on     TEXT NOT NULL,
	created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_expenses_spent_on ON expenses (spent_on);

CREATE TABLE IF NOT EXISTS reminders (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT NOT NULL,
	due_date   TEXT NOT NULL,
	due_time   TEXT NOT NULL DEFAULT '',
	repeat     TEXT NOT NULL DEFAULT '',
	done       INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0, 1)),
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reminders_open ON reminders (done, due_date);

CREATE TABLE IF NOT EXISTS budgets (
	category    TEXT PRIMARY KEY,
	limit_paise INTEGER NOT NULL CHECK (limit_paise > 0),
	updated_at  TEXT NOT NULL
);
