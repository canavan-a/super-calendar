-- +goose Up

-- A calendar of one kind, shared by its members. Nobody owns it: every
-- member can edit everything, invite and leave.
CREATE TABLE calendars (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('general', 'training', 'cycle')),
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    -- Per-kind settings as JSON: training {weekStart, planStart}.
    settings   TEXT NOT NULL DEFAULT '{}',
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Membership plus each member's own view of the calendar.
CREATE TABLE calendar_members (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    color       TEXT,                           -- personal override, NULL = the calendar's
    in_overview INTEGER NOT NULL DEFAULT 1,
    position    INTEGER NOT NULL DEFAULT 0,     -- order in the switcher
    joined_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (calendar_id, user_id)
);
CREATE INDEX idx_calendar_members_user ON calendar_members(user_id);

-- The last member to go (leaving, or deleting their account, which cascades
-- here) takes the calendar and everything in it with them.
-- +goose StatementBegin
CREATE TRIGGER calendar_last_member_gone
AFTER DELETE ON calendar_members
WHEN NOT EXISTS (SELECT 1 FROM calendar_members WHERE calendar_id = OLD.calendar_id)
BEGIN
    DELETE FROM calendars WHERE id = OLD.calendar_id;
END;
-- +goose StatementEnd

-- Invites match people by email, so one sent before someone has an account
-- shows up in their Inbox once they sign up.
CREATE TABLE calendar_invites (
    id           TEXT PRIMARY KEY,
    calendar_id  TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    inviter_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    email        TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled')),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    responded_at TEXT
);
CREATE UNIQUE INDEX idx_calendar_invites_pending
    ON calendar_invites(calendar_id, email) WHERE status = 'pending';
CREATE INDEX idx_calendar_invites_email ON calendar_invites(email, status);

-- Per-user preferences as JSON (Overview layer switches).
CREATE TABLE user_settings (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    settings   TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- General calendars. Timed events store UTC instants plus the zone they were
-- made in, so recurrences keep their wall-clock time across DST; all-day
-- events store dates. end is exclusive.
CREATE TABLE events (
    id            TEXT PRIMARY KEY,
    calendar_id   TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid           TEXT NOT NULL,
    -- Set on an override of one occurrence of a series: the original start
    -- of that occurrence, in the same format as start.
    recurrence_id TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    location      TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    all_day       INTEGER NOT NULL DEFAULT 0,
    start         TEXT NOT NULL,
    end           TEXT NOT NULL,
    tzid          TEXT NOT NULL DEFAULT 'UTC',
    rrule         TEXT NOT NULL DEFAULT '',
    exdates       TEXT NOT NULL DEFAULT '[]',
    -- An override can cancel its occurrence instead of moving it.
    cancelled     INTEGER NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'ics')),
    created_by    TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_by    TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (calendar_id, uid, recurrence_id)
);
CREATE INDEX idx_events_calendar ON events(calendar_id, start);

-- Training calendars: the activity library and the sessions.
CREATE TABLE activity_types (
    id          TEXT PRIMARY KEY,
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL,
    icon        TEXT NOT NULL DEFAULT '',
    -- Which plan fields this type asks for: duration, distance, intensity, exercises.
    fields      TEXT NOT NULL DEFAULT '[]',
    archived    INTEGER NOT NULL DEFAULT 0,
    position    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_activity_types_calendar ON activity_types(calendar_id);

CREATE TABLE training_sessions (
    id               TEXT PRIMARY KEY,
    calendar_id      TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    date             TEXT NOT NULL,
    time             TEXT NOT NULL DEFAULT '',
    position         INTEGER NOT NULL DEFAULT 0,
    activity_type_id TEXT REFERENCES activity_types(id) ON DELETE SET NULL,
    title            TEXT NOT NULL,
    plan             TEXT NOT NULL DEFAULT '{}',
    notes            TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'done', 'skipped')),
    result           TEXT NOT NULL DEFAULT '{}',
    created_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_training_sessions_calendar ON training_sessions(calendar_id, date);

-- Cycle calendars: one row per logged day; unlogged days have no row.
CREATE TABLE cycle_days (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,
    flow        TEXT NOT NULL CHECK (flow IN ('none', 'spotting', 'light', 'medium', 'heavy')),
    notes       TEXT NOT NULL DEFAULT '',
    updated_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (calendar_id, date)
);

-- +goose Down
DROP TABLE cycle_days;
DROP TABLE training_sessions;
DROP TABLE activity_types;
DROP TABLE events;
DROP TABLE user_settings;
DROP TABLE calendar_invites;
DROP TRIGGER calendar_last_member_gone;
DROP TABLE calendar_members;
DROP TABLE calendars;
