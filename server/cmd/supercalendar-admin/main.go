// Command supercalendar-admin manages the cap on accounts, and has a couple
// of calendar tools. Anyone can sign up until there are that many accounts
// (50 to start); after that only people with an account, or a pending
// calendar invite, can sign in.
//
//	supercalendar-admin cap              how many accounts there are, and the cap
//	supercalendar-admin cap <n>          set the cap (raise it to let more in)
//	supercalendar-admin list             everyone with an account, oldest first
//	supercalendar-admin remove <email>   delete that account and all its data
//	supercalendar-admin calendars        every calendar: id, type, members, name
//	supercalendar-admin cycle-backtest <calendar-id>
//	                                     how far off period predictions would have
//	                                     been on that cycle calendar's history
//
// It works on the database the server uses (SUPERCALENDAR_DB_PATH or
// SUPERCALENDAR_DATA_DIR, as for the server), and changes apply immediately.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/auth"
	"github.com/canavan-a/super-calendar/server/internal/config"
	"github.com/canavan-a/super-calendar/server/internal/cycle"
	"github.com/canavan-a/super-calendar/server/internal/db"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cfg := config.Load()
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		fail(err)
	}
	conn, err := db.Connect(ctx, cfg.DatabasePath)
	if err != nil {
		fail(err)
	}
	defer conn.Close()
	// So it works before the server has ever run.
	if err := db.Migrate(ctx, conn); err != nil {
		fail(err)
	}
	store := auth.NewStore(conn)

	args := os.Args[2:]
	switch os.Args[1] {
	case "cap":
		switch len(args) {
		case 0:
		case 1:
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 0 {
				fail(fmt.Errorf("%q isn't a number of accounts", args[0]))
			}
			if err := store.SetUserCap(ctx, n); err != nil {
				fail(err)
			}
		default:
			usage()
		}
		count, err := store.CountUsers(ctx)
		if err != nil {
			fail(err)
		}
		limit, err := store.UserCap(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%d of %d accounts", count, limit)
		if count >= limit {
			fmt.Print(" (full: nobody new can sign up)")
		}
		fmt.Println()
	case "list":
		if len(args) != 0 {
			usage()
		}
		users, err := store.ListUsers(ctx)
		if err != nil {
			fail(err)
		}
		if len(users) == 0 {
			fmt.Println("nobody has signed up yet")
		}
		for _, u := range users {
			fmt.Printf("%s\t%s\t%s\n", u.CreatedAt.Format("2006-01-02"), u.Email, u.DisplayName)
		}
	case "remove":
		if len(args) != 1 {
			usage()
		}
		email := auth.NormalizeEmail(args[0])
		user, err := store.GetUserByEmail(ctx, email)
		if errors.Is(err, auth.ErrNotFound) {
			fail(fmt.Errorf("%s has no account", email))
		}
		if err != nil {
			fail(err)
		}
		if err := store.DeleteUser(ctx, user.ID); err != nil {
			fail(err)
		}
		fmt.Println("deleted", email, "(signed out everywhere, they can sign up again while there's room)")
	case "calendars":
		rows, err := conn.QueryContext(ctx, `SELECT c.id, c.kind, (SELECT COUNT(*) FROM calendar_members m WHERE m.calendar_id = c.id), c.name
			FROM calendars c ORDER BY c.created_at`)
		if err != nil {
			fail(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, kind, name string
			var n int
			if err := rows.Scan(&id, &kind, &n, &name); err != nil {
				fail(err)
			}
			fmt.Printf("%s\t%s\t%d\t%s\n", id, kind, n, name)
		}
	case "cycle-backtest":
		if len(args) != 1 {
			usage()
		}
		backtest(ctx, conn, args[0])
	default:
		usage()
	}
}

// backtest prints only the error figures, none of the logged days.
func backtest(ctx context.Context, conn *sql.DB, calendarID string) {
	rows, err := conn.QueryContext(ctx, `SELECT date, flow FROM cycle_days WHERE calendar_id = ?`, calendarID)
	if err != nil {
		fail(err)
	}
	defer rows.Close()
	var days []cycle.Day
	for rows.Next() {
		var date, flow string
		if err := rows.Scan(&date, &flow); err != nil {
			fail(err)
		}
		if t, err := time.Parse(cycle.DateLayout, date); err == nil {
			days = append(days, cycle.Day{Date: t, Flow: flow})
		}
	}
	mae, n := cycle.Backtest(days)
	if n == 0 {
		fmt.Println("not enough history to test: it needs at least 4 logged periods")
		return
	}
	fmt.Printf("%d predictions, off by %.1f days on average\n", n, mae)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: supercalendar-admin cap [<n>] | list | remove <email> | calendars | cycle-backtest <calendar-id>")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "supercalendar-admin:", err)
	os.Exit(1)
}
