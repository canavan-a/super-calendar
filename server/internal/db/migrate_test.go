package db

import (
	"context"
	"path/filepath"
	"testing"
)

// The subscribed-calendars migration rebuilds calendars, which everything
// else references with ON DELETE CASCADE: done wrong, it empties the database.
func TestCalendarsRebuildKeepsEverything(t *testing.T) {
	ctx := context.Background()
	conn, err := Connect(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := migrate(ctx, conn, "0003_event_color.sql"); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, email, display_name) VALUES ('u1', 'a@x.co', 'Ana'), ('u2', 'b@x.co', 'Bo')`,
		`INSERT INTO calendars (id, kind, name, color, settings, created_by) VALUES
			('g', 'general', 'Family', '#336699', '{}', 'u1'),
			('t', 'training', 'Plan', '#336699', '{"weekStart":1}', 'u1'),
			('c', 'cycle', 'Ana', '#336699', '{}', 'u1')`,
		`INSERT INTO calendar_members (calendar_id, user_id, position) VALUES ('g', 'u1', 0), ('g', 'u2', 0), ('t', 'u1', 1), ('c', 'u1', 2)`,
		`INSERT INTO calendar_invites (id, calendar_id, inviter_id, email) VALUES ('i1', 'g', 'u1', 'c@x.co')`,
		`INSERT INTO events (id, calendar_id, uid, title, start, end) VALUES ('e1', 'g', 'uid1', 'Dinner', '2026-01-01', '2026-01-02')`,
		`INSERT INTO activity_types (id, calendar_id, name, color) VALUES ('a1', 't', 'Run', '#ff0000')`,
		`INSERT INTO training_sessions (id, calendar_id, date, activity_type_id, title) VALUES ('s1', 't', '2026-01-01', 'a1', 'Easy')`,
		`INSERT INTO cycle_days (calendar_id, date, flow) VALUES ('c', '2026-01-01', 'light')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{
		"calendars": 3, "calendar_members": 4, "calendar_invites": 1, "events": 1,
		"activity_types": 1, "training_sessions": 1, "cycle_days": 1,
	}
	for table, want := range counts {
		var got int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: %d rows after the migration, want %d", table, got, want)
		}
	}
	var settings string
	if err := conn.QueryRowContext(ctx, `SELECT settings FROM calendars WHERE id = 't'`).Scan(&settings); err != nil || settings != `{"weekStart":1}` {
		t.Errorf("settings %q, %v", settings, err)
	}

	// Foreign keys are back on, the new kind is allowed, and the cascades and
	// the last-member trigger still work.
	var fk int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v", fk, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO calendars (id, kind, name, color) VALUES ('s', 'subscribed', 'Work', '#336699')`); err != nil {
		t.Fatalf("subscribed kind: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO calendars (id, kind, name, color) VALUES ('x', 'nonsense', 'X', '#336699')`); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM calendar_members WHERE calendar_id = 'g'`); err != nil {
		t.Fatal(err)
	}
	for table, where := range map[string]string{"calendars": "id = 'g'", "events": "calendar_id = 'g'", "calendar_invites": "calendar_id = 'g'"} {
		var n int
		_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE `+where).Scan(&n)
		if n != 0 {
			t.Errorf("%s: %d rows left after the last member left", table, n)
		}
	}
}
