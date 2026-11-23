-- +goose Up
-- +nofk

-- Subscribed calendars: a fourth kind that follows an iCal link. The kind
-- CHECK on calendars can't be changed in place, so the table is rebuilt
-- (SQLite's 12-step procedure; +nofk runs this with foreign keys off, so
-- dropping the old table doesn't cascade into everything inside calendars).
-- The trigger names calendars, so it goes first and comes back after.
DROP TRIGGER calendar_last_member_gone;

CREATE TABLE calendars_new (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('general', 'training', 'cycle', 'subscribed')),
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    -- Per-kind settings as JSON: training {weekStart, planStart}.
    settings   TEXT NOT NULL DEFAULT '{}',
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO calendars_new (id, kind, name, color, settings, created_by, created_at, updated_at)
    SELECT id, kind, name, color, settings, created_by, created_at, updated_at FROM calendars;
DROP TABLE calendars;
ALTER TABLE calendars_new RENAME TO calendars;

-- +goose StatementBegin
CREATE TRIGGER calendar_last_member_gone
AFTER DELETE ON calendar_members
WHEN NOT EXISTS (SELECT 1 FROM calendar_members WHERE calendar_id = OLD.calendar_id)
BEGIN
    DELETE FROM calendars WHERE id = OLD.calendar_id;
END;
-- +goose StatementEnd

-- The link a subscribed calendar follows, and how syncing it is going. The
-- URL is a secret (whoever has it can read the calendar), so it lives here
-- rather than in calendars.settings, which every member is sent.
CREATE TABLE calendar_feeds (
    calendar_id    TEXT PRIMARY KEY REFERENCES calendars(id) ON DELETE CASCADE,
    url            TEXT NOT NULL,
    tz             TEXT NOT NULL DEFAULT 'UTC', -- for times the feed doesn't zone: the zone of whoever added it
    added_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    -- ok; failing (passing trouble, retried with backoff); broken (the link
    -- answers 401/403/404/410, as a reset secret link does: retried daily
    -- until it's replaced).
    status         TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'failing', 'broken')),
    last_error     TEXT NOT NULL DEFAULT '',   -- fit to show; never contains the URL
    failures       INTEGER NOT NULL DEFAULT 0, -- in a row, for backoff
    empty_fetches  INTEGER NOT NULL DEFAULT 0, -- in a row: one empty feed doesn't empty the calendar
    etag           TEXT NOT NULL DEFAULT '',
    last_modified  TEXT NOT NULL DEFAULT '',
    body_hash      TEXT NOT NULL DEFAULT '',   -- an unchanged feed writes nothing
    last_synced_at TEXT,                       -- the last successful sync
    last_refresh_at TEXT,                      -- the last "Refresh now", to rate-limit it
    next_sync_at   TEXT NOT NULL,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_calendar_feeds_next ON calendar_feeds(next_sync_at);

-- +goose Down
DROP TABLE calendar_feeds;
