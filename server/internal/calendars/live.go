package calendars

import (
	"context"
	"net/http"
	"strings"

	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/go-chi/chi/v5"
)

// Live updates: after any change inside a calendar, every member's open apps
// are told (internal/live) so they refetch. This file decides who hears what.

// UseLive sends change notifications through hub (nil turns them off).
func (s *Service) UseLive(hub *live.Hub) { s.live = hub }

func (s *Service) memberIDs(ctx context.Context, calendarID string) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM calendar_members WHERE calendar_id = ?`, calendarID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) displayName(ctx context.Context, userID string) string {
	var name string
	_ = s.db.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id = ?`, userID).Scan(&name)
	return name
}

// send delivers m to userIDs, if live updates are on.
func (s *Service) send(userIDs []string, m live.Message) {
	if s.live != nil && len(userIDs) > 0 {
		s.live.Send(userIDs, m)
	}
}

// toSelf tells the user's own apps (other phones, and the one that made the
// change, which ignores it by Origin) that their calendar list or Inbox changed.
func (s *Service) toSelf(r *http.Request, kind string) {
	s.send([]string{userID(r)}, live.Message{Type: kind, Origin: origin(r)})
}

// toInbox tells whoever has email that their Inbox changed (if they have an
// account yet).
func (s *Service) toInbox(ctx context.Context, email string) {
	var id string
	if s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id) == nil {
		s.send([]string{id}, live.Message{Type: "inbox"})
	}
}

// origin is the id the app sends with its requests (X-Live-Client), so the
// notification its own change causes can be told apart.
func origin(r *http.Request) string {
	o := r.Header.Get("X-Live-Client")
	if len(o) > 64 {
		return ""
	}
	return o
}

// areaFor names what a request under /calendars/{id} changes, from its path.
func areaFor(r *http.Request) string {
	rest := r.URL.Path
	if i := strings.Index(rest, "/calendars/"); i >= 0 {
		rest = rest[i+len("/calendars/"):]
	}
	_, rest, _ = strings.Cut(rest, "/")
	head, _, _ := strings.Cut(rest, "/")
	switch head {
	case "":
		return "calendar"
	case "me":
		return "me"
	case "members":
		return "members"
	case "invites":
		return "invites"
	case "events", "import":
		return "events"
	case "sessions", "activity-types":
		return "training"
	case "cycle":
		return "cycle"
	}
	return "calendar"
}

// itemID is the one thing a request changed, when it names one.
func itemID(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return ""
	}
	for _, key := range []string{"eventID", "sessionID", "typeID", "date"} {
		if v := rc.URLParam(key); v != "" {
			return v
		}
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Notify runs after RequireMember: once a change inside the calendar has
// succeeded, its members (before and after it, so someone who just left or
// joined hears too) are told what changed.
func (h *Handlers) Notify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || h.s.live == nil {
			next.ServeHTTP(w, r)
			return
		}
		cal := calendarFrom(r)
		before := h.s.memberIDs(r.Context(), cal.ID)
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status < 200 || sw.status >= 300 {
			return
		}
		ctx := context.WithoutCancel(r.Context())
		area := areaFor(r)
		me := userID(r)
		if area == "me" {
			// Your own colour, Overview switch or leaving: your list changed.
			// Leaving changes everyone else's member list too.
			h.s.toSelf(r, "calendars")
			if r.Method != http.MethodDelete {
				return
			}
			area = "members"
		}
		after := h.s.memberIDs(ctx, cal.ID)
		recipients := union(before, after)
		if r.Method == http.MethodDelete && areaFor(r) == "me" {
			// Whoever left only needs their own list updated (sent above).
			recipients = after
		}
		h.s.send(recipients, live.Message{
			Type: "changed", CalendarID: cal.ID, Area: area, ItemID: itemID(r),
			By: me, ByName: h.s.displayName(ctx, me), Origin: origin(r),
		})
	})
}
