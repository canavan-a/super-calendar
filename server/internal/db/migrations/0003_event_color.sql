-- +goose Up
-- An event's own colour; '' uses its calendar's.
ALTER TABLE events ADD COLUMN color TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN color;
