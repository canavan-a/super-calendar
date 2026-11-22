// Package calendars is everything inside a calendar: membership and invites
// (shared by every kind), then each kind's own data (events, training
// sessions, cycle days) and the Overview that overlays them.
package calendars

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/canavan-a/super-calendar/server/internal/ratelimit"
	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrAlreadyMember: inviting someone who is in the calendar already.
	ErrAlreadyMember = errors.New("already a member")
)

// InvalidError is a bad request with a message fit to show the user.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// ConflictError: an edit was based on a version someone has since changed.
// Current is what's there now and By who saved it, so the app can ask
// whether to keep theirs or overwrite.
type ConflictError struct {
	Current any
	By      string
}

func (e *ConflictError) Error() string { return "changed by someone else" }

// stale reports whether an edit based on base would overwrite a newer save.
func stale(base *string, current string) bool {
	return base != nil && *base != "" && *base != current
}

func invalid(format string, args ...any) error {
	return &InvalidError{Msg: fmt.Sprintf(format, args...)}
}

const (
	General  = "general"
	Training = "training"
	Cycle    = "cycle"
)

func validKind(k string) bool { return k == General || k == Training || k == Cycle }

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

const timeLayout = "2006-01-02T15:04:05.000Z"

func stamp(t time.Time) string { return t.UTC().Format(timeLayout) }

// Invite limits. An unanswered invite expires after InviteTTL; each member can
// create InvitesPerDay a day; invite emails from everyone together are capped
// per hour (past that the invite still lands in the Inbox, unemailed).
const (
	InviteTTL           = 14 * 24 * time.Hour
	InvitesPerDay       = 20
	inviteEmailsPerHour = 100
)

// inviteCutoff is when a still-pending invite made earlier has expired.
func (s *Service) inviteCutoff() string { return stamp(s.now().Add(-InviteTTL)) }

// Mailer sends invite emails; satisfied by mail.Sender.
type Mailer interface {
	SendInvite(to, inviterName, calendarName, kind string) error
}

type Service struct {
	db     *sql.DB
	mailer Mailer
	now    func() time.Time
	// Invite emails from everyone together (inviteEmailsPerHour).
	inviteEmails *ratelimit.Limiter
	fetcher      Fetcher   // nil: fetch links over the internet
	live         *live.Hub // nil: no live updates (live.go)
}

func NewService(db *sql.DB, mailer Mailer) *Service {
	return &Service{db: db, mailer: mailer, now: time.Now, inviteEmails: ratelimit.New(inviteEmailsPerHour, time.Hour)}
}

// Calendar is a calendar as one member sees it: the shared fields plus their
// own colour, Overview switch and place in the list.
type Calendar struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Color       string          `json:"color"`
	MyColor     *string         `json:"myColor"`
	InOverview  bool            `json:"inOverview"`
	Position    int             `json:"position"`
	Settings    json.RawMessage `json:"settings"`
	MemberCount int             `json:"memberCount"`
	CreatedAt   string          `json:"createdAt"`
}

// DisplayColor is the member's own colour if they picked one.
func (c *Calendar) DisplayColor() string {
	if c.MyColor != nil {
		return *c.MyColor
	}
	return c.Color
}

const calendarCols = `c.id, c.kind, c.name, c.color, m.color, m.in_overview, m.position, c.settings,
	(SELECT COUNT(*) FROM calendar_members x WHERE x.calendar_id = c.id), c.created_at`

func scanCalendar(row interface{ Scan(...any) error }) (*Calendar, error) {
	var c Calendar
	var settings string
	var myColor sql.NullString
	if err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.Color, &myColor, &c.InOverview, &c.Position, &settings, &c.MemberCount, &c.CreatedAt); err != nil {
		return nil, err
	}
	if myColor.Valid {
		c.MyColor = &myColor.String
	}
	c.Settings = json.RawMessage(settings)
	return &c, nil
}

// Get returns calendarID as userID sees it, or ErrNotFound if they aren't a
// member: a non-member can't tell the calendar exists.
func (s *Service) Get(ctx context.Context, calendarID, userID string) (*Calendar, error) {
	c, err := scanCalendar(s.db.QueryRowContext(ctx, `SELECT `+calendarCols+`
		FROM calendars c JOIN calendar_members m ON m.calendar_id = c.id AND m.user_id = ?
		WHERE c.id = ?`, userID, calendarID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Service) List(ctx context.Context, userID string) ([]*Calendar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+calendarCols+`
		FROM calendars c JOIN calendar_members m ON m.calendar_id = c.id AND m.user_id = ?
		ORDER BY m.position, c.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Calendar{}
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return "", invalid("give it a name of up to 80 characters")
	}
	return name, nil
}

func (s *Service) Create(ctx context.Context, userID, kind, name, color string) (*Calendar, error) {
	if !validKind(kind) {
		return nil, invalid("unknown calendar type")
	}
	name, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	if !colorRe.MatchString(color) {
		return nil, invalid("colour must look like #a1b2c3")
	}
	id := uuid.NewString()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO calendars (id, kind, name, color, settings, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?, ?, ?)`, id, kind, name, color, userID, now, now); err != nil {
		return nil, err
	}
	if err := addMember(ctx, tx, id, userID, now); err != nil {
		return nil, err
	}
	if kind == Training {
		if err := seedActivityTypes(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id, userID)
}

// addMember puts a new member at the end of their list.
func addMember(ctx context.Context, tx *sql.Tx, calendarID, userID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO calendar_members (calendar_id, user_id, position, joined_at)
		VALUES (?, ?, (SELECT COALESCE(MAX(position) + 1, 0) FROM calendar_members WHERE user_id = ?), ?)
		ON CONFLICT DO NOTHING`, calendarID, userID, userID, now)
	return err
}

type CalendarPatch struct {
	Name     *string          `json:"name"`
	Color    *string          `json:"color"`
	Settings *json.RawMessage `json:"settings"`
}

// Update changes the shared fields, which any member may do. Settings are
// merged key by key into what's there.
func (s *Service) Update(ctx context.Context, cal *Calendar, p CalendarPatch) error {
	name, color, settings := cal.Name, cal.Color, string(cal.Settings)
	var err error
	if p.Name != nil {
		if name, err = cleanName(*p.Name); err != nil {
			return err
		}
	}
	if p.Color != nil {
		if !colorRe.MatchString(*p.Color) {
			return invalid("colour must look like #a1b2c3")
		}
		color = *p.Color
	}
	if p.Settings != nil {
		if settings, err = mergeSettings(cal.Kind, settings, *p.Settings); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE calendars SET name = ?, color = ?, settings = ?, updated_at = ? WHERE id = ?`,
		name, color, settings, stamp(s.now()), cal.ID)
	return err
}

func mergeSettings(kind, current string, patch json.RawMessage) (string, error) {
	cur := map[string]any{}
	_ = json.Unmarshal([]byte(current), &cur)
	var p map[string]any
	if err := json.Unmarshal(patch, &p); err != nil {
		return "", invalid("settings must be an object")
	}
	for k, v := range p {
		if kind == Training {
			switch k {
			case "weekStart":
				n, ok := v.(float64)
				if !ok || (n != 0 && n != 1) {
					return "", invalid("weekStart is 0 (Sunday) or 1 (Monday)")
				}
			case "planStart":
				if v != nil {
					str, _ := v.(string)
					if _, err := time.Parse(dateLayout, str); err != nil {
						return "", invalid("planStart must be a date")
					}
				}
			default:
				return "", invalid("unknown setting %q", k)
			}
		} else {
			return "", invalid("this calendar has no settings")
		}
		if v == nil {
			delete(cur, k)
		} else {
			cur[k] = v
		}
	}
	b, _ := json.Marshal(cur)
	return string(b), nil
}

type MemberPatch struct {
	// Color "" clears the personal colour.
	Color      *string `json:"color"`
	InOverview *bool   `json:"inOverview"`
	Position   *int    `json:"position"`
}

func (s *Service) UpdateMine(ctx context.Context, cal *Calendar, userID string, p MemberPatch) error {
	var myColor any
	if cal.MyColor != nil {
		myColor = *cal.MyColor
	}
	inOverview, position := cal.InOverview, cal.Position
	if p.Color != nil {
		switch {
		case *p.Color == "":
			myColor = nil
		case colorRe.MatchString(*p.Color):
			myColor = *p.Color
		default:
			return invalid("colour must look like #a1b2c3")
		}
	}
	if p.InOverview != nil {
		inOverview = *p.InOverview
	}
	if p.Position != nil {
		position = *p.Position
	}
	_, err := s.db.ExecContext(ctx, `UPDATE calendar_members SET color = ?, in_overview = ?, position = ?
		WHERE calendar_id = ? AND user_id = ?`, myColor, inOverview, position, cal.ID, userID)
	return err
}

// Reorder sets the switcher order to ids (the user's calendars, top first).
func (s *Service) Reorder(ctx context.Context, userID string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE calendar_members SET position = ? WHERE calendar_id = ? AND user_id = ?`, i, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Leave removes userID from the calendar. The last member leaving deletes it
// (a trigger does that, so deleting an account does too).
func (s *Service) Leave(ctx context.Context, calendarID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM calendar_members WHERE calendar_id = ? AND user_id = ?`, calendarID, userID)
	return err
}

type Member struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	JoinedAt    string `json:"joinedAt"`
}

func (s *Service) Members(ctx context.Context, calendarID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.display_name, u.email, m.joined_at
		FROM calendar_members m JOIN users u ON u.id = m.user_id
		WHERE m.calendar_id = ? ORDER BY m.joined_at`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.DisplayName, &m.Email, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Invite struct {
	ID            string `json:"id"`
	CalendarID    string `json:"calendarId"`
	CalendarName  string `json:"calendarName"`
	CalendarKind  string `json:"calendarKind"`
	CalendarColor string `json:"calendarColor"`
	Email         string `json:"email"`
	InviterName   string `json:"inviterName"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
}

const inviteCols = `i.id, i.calendar_id, c.name, c.kind, c.color, i.email, COALESCE(u.display_name, ''), i.status, i.created_at`

const inviteFrom = ` FROM calendar_invites i JOIN calendars c ON c.id = i.calendar_id
	LEFT JOIN users u ON u.id = i.inviter_id `

func scanInvites(rows *sql.Rows) ([]Invite, error) {
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.ID, &i.CalendarID, &i.CalendarName, &i.CalendarKind, &i.CalendarColor, &i.Email, &i.InviterName, &i.Status, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Invite asks email to join the calendar. Inviting the same email again while
// one is pending returns the pending invite (and doesn't email again).
func (s *Service) Invite(ctx context.Context, cal *Calendar, inviterID, email string) (*Invite, error) {
	email = normalizeEmail(email)
	if !strings.Contains(email, "@") || len(email) > 254 {
		return nil, invalid("enter a valid email")
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM calendar_members m JOIN users u ON u.id = m.user_id
		WHERE m.calendar_id = ? AND u.email = ?`, cal.ID, email).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrAlreadyMember
	}
	now := s.now()
	// An expired invite to this email makes way for the new one.
	if _, err := s.db.ExecContext(ctx, `UPDATE calendar_invites SET status = 'cancelled', responded_at = ?
		WHERE calendar_id = ? AND email = ? AND status = 'pending' AND created_at < ?`,
		stamp(now), cal.ID, email, stamp(now.Add(-InviteTTL))); err != nil {
		return nil, err
	}
	// A pending invite lets its email sign up past the account cap and invite
	// code, so each member can only make so many.
	var sent int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM calendar_invites WHERE inviter_id = ? AND created_at >= ?
		AND NOT EXISTS (SELECT 1 FROM calendar_invites p WHERE p.calendar_id = ? AND p.email = ? AND p.status = 'pending')`,
		inviterID, stamp(now.Add(-24*time.Hour)), cal.ID, email).Scan(&sent); err != nil {
		return nil, err
	}
	if sent >= InvitesPerDay {
		return nil, invalid("you've sent %d invites today, try again tomorrow", InvitesPerDay)
	}
	id := uuid.NewString()
	res, err := s.db.ExecContext(ctx, `INSERT INTO calendar_invites (id, calendar_id, inviter_id, email, created_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, id, cal.ID, inviterID, email, stamp(now))
	if err != nil {
		return nil, err
	}
	created, _ := res.RowsAffected()
	invites, err := s.queryInvites(ctx, `WHERE i.calendar_id = ? AND i.email = ? AND i.status = 'pending'`, cal.ID, email)
	if err != nil {
		return nil, err
	}
	if len(invites) == 0 {
		return nil, ErrNotFound
	}
	inv := invites[0]
	if created > 0 {
		s.toInbox(ctx, email)
	}
	if created > 0 && s.mailer != nil && !s.inviteEmails.Allow("all") {
		log.Printf("invite email to %s skipped: over %d an hour (the invite is in their Inbox)", email, inviteEmailsPerHour)
	} else if created > 0 && s.mailer != nil {
		// The invite stands even if the email doesn't go: it's in their Inbox.
		if err := s.mailer.SendInvite(email, inv.InviterName, cal.Name, cal.Kind); err != nil {
			log.Printf("invite email to %s: %v", email, err)
		}
	}
	return &inv, nil
}

// queryInvites lists the invites matching where.
func (s *Service) queryInvites(ctx context.Context, where string, args ...any) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteCols+inviteFrom+where, args...)
	if err != nil {
		return nil, err
	}
	return scanInvites(rows)
}

func (s *Service) SentInvites(ctx context.Context, calendarID string) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteCols+inviteFrom+`
		WHERE i.calendar_id = ? AND i.status = 'pending' AND i.created_at >= ? ORDER BY i.created_at`, calendarID, s.inviteCutoff())
	if err != nil {
		return nil, err
	}
	return scanInvites(rows)
}

func (s *Service) CancelInvite(ctx context.Context, calendarID, inviteID string) error {
	var email string
	_ = s.db.QueryRowContext(ctx, `SELECT email FROM calendar_invites WHERE id = ? AND calendar_id = ?`, inviteID, calendarID).Scan(&email)
	res, err := s.db.ExecContext(ctx, `UPDATE calendar_invites SET status = 'cancelled', responded_at = ?
		WHERE id = ? AND calendar_id = ? AND status = 'pending'`, stamp(s.now()), inviteID, calendarID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.toInbox(ctx, email)
	return nil
}

func (s *Service) userEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, userID).Scan(&email)
	return email, err
}

// Inbox is the pending invites for userID's email.
func (s *Service) Inbox(ctx context.Context, userID string) ([]Invite, error) {
	email, err := s.userEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteCols+inviteFrom+`
		WHERE i.email = ? AND i.status = 'pending' AND i.created_at >= ? ORDER BY i.created_at DESC`, email, s.inviteCutoff())
	if err != nil {
		return nil, err
	}
	return scanInvites(rows)
}

// Respond accepts or declines an invite addressed to userID's email.
// Accepting returns the calendar they've joined.
func (s *Service) Respond(ctx context.Context, userID, inviteID string, accept bool) (*Calendar, error) {
	email, err := s.userEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var calendarID string
	err = tx.QueryRowContext(ctx, `SELECT calendar_id FROM calendar_invites WHERE id = ? AND email = ? AND status = 'pending' AND created_at >= ?`,
		inviteID, email, s.inviteCutoff()).Scan(&calendarID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	status := "declined"
	if accept {
		status = "accepted"
	}
	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_invites SET status = ?, responded_at = ? WHERE id = ?`, status, now, inviteID); err != nil {
		return nil, err
	}
	if accept {
		if err := addMember(ctx, tx, calendarID, userID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if !accept {
		return nil, nil
	}
	return s.Get(ctx, calendarID, userID)
}

// UserSettings is the user's own preferences as a JSON object (Overview
// layer switches), stored as the app sends it.
func (s *Service) UserSettings(ctx context.Context, userID string) (json.RawMessage, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT settings FROM user_settings WHERE user_id = ?`, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(v), err
}

func (s *Service) SetUserSettings(ctx context.Context, userID string, v json.RawMessage) error {
	var obj map[string]any
	if err := json.Unmarshal(v, &obj); err != nil {
		return invalid("settings must be an object")
	}
	if len(v) > 16<<10 {
		return invalid("settings are too large")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_settings (user_id, settings, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET settings = excluded.settings, updated_at = excluded.updated_at`,
		userID, string(v), stamp(s.now()))
	return err
}
