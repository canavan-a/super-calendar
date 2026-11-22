package calendars

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teambition/rrule-go"
)

const dateLayout = "2006-01-02"

// maxOccurrences caps how many occurrences one series yields per query, and
// maxSteps how many it walks to get there (from the series' first one): a
// daily series 100 years old is about 36,500 steps.
const (
	maxOccurrences = 500
	maxSteps       = 50000
)

// Range is a span of local dates [From, To) in the viewer's zone, with the
// instants it covers.
type Range struct {
	FromDate, ToDate string
	From, To         time.Time
	Loc              *time.Location
}

// maxRangeDays caps a query (a few months of Overview, with room to spare).
const maxRangeDays = 400

func ParseRange(from, to, tz string) (Range, error) {
	loc := time.UTC
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return Range{}, invalid("unknown time zone")
		}
		loc = l
	}
	f, err1 := time.ParseInLocation(dateLayout, from, loc)
	t, err2 := time.ParseInLocation(dateLayout, to, loc)
	if err1 != nil || err2 != nil || !t.After(f) {
		return Range{}, invalid("from and to must be dates, from before to")
	}
	if t.Sub(f) > maxRangeDays*24*time.Hour {
		return Range{}, invalid("ask for at most %d days at a time", maxRangeDays)
	}
	return Range{FromDate: from, ToDate: to, From: f, To: t, Loc: loc}, nil
}

type Event struct {
	ID           string `json:"id"`
	CalendarID   string `json:"calendarId"`
	UID          string `json:"uid"`
	RecurrenceID string `json:"recurrenceId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Location     string `json:"location"`
	URL          string `json:"url"`
	// Color is the event's own colour, or "" for its calendar's.
	Color     string   `json:"color"`
	AllDay    bool     `json:"allDay"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	TZID      string   `json:"tzid"`
	RRule     string   `json:"rrule"`
	ExDates   []string `json:"exdates"`
	Cancelled bool     `json:"cancelled"`
	Source    string   `json:"source"`
	UpdatedBy string   `json:"updatedBy"`
	UpdatedAt string   `json:"updatedAt"`
}

const eventCols = `e.id, e.calendar_id, e.uid, e.recurrence_id, e.title, e.description, e.location, e.url,
	e.all_day, e.start, e.end, e.tzid, e.rrule, e.exdates, e.cancelled, e.source, e.color,
	COALESCE(u.display_name, ''), e.updated_at`

const eventFrom = ` FROM events e LEFT JOIN users u ON u.id = e.updated_by `

func scanEvent(row interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	var exdates string
	if err := row.Scan(&e.ID, &e.CalendarID, &e.UID, &e.RecurrenceID, &e.Title, &e.Description, &e.Location, &e.URL,
		&e.AllDay, &e.Start, &e.End, &e.TZID, &e.RRule, &exdates, &e.Cancelled, &e.Source, &e.Color, &e.UpdatedBy, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.ExDates = []string{}
	_ = json.Unmarshal([]byte(exdates), &e.ExDates)
	return &e, nil
}

func (s *Service) queryEvents(ctx context.Context, where string, args ...any) ([]*Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+eventFrom+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) GetEvent(ctx context.Context, calendarID, eventID string) (*Event, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+eventFrom+`WHERE e.calendar_id = ? AND e.id = ?`, calendarID, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// EventInput is what the app sends to make or change an event. Timed starts
// and ends are RFC3339 with any offset; all-day ones are dates, end exclusive
// (empty means one day).
type EventInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Location    *string `json:"location"`
	URL         *string `json:"url"`
	Color       *string `json:"color"` // "" uses the calendar's
	AllDay      *bool   `json:"allDay"`
	Start       *string `json:"start"`
	End         *string `json:"end"`
	TZID        *string `json:"tzid"`
	RRule       *string `json:"rrule"`
	// BaseUpdatedAt is the updatedAt the editor loaded: if the event has been
	// saved since (by someone else), the update is refused with a
	// ConflictError instead of overwriting their change. Leave it out to
	// overwrite anyway.
	BaseUpdatedAt *string `json:"baseUpdatedAt"`
}

func (in EventInput) apply(e *Event) error {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&e.Title, in.Title)
	set(&e.Description, in.Description)
	set(&e.Location, in.Location)
	set(&e.URL, in.URL)
	set(&e.Color, in.Color)
	set(&e.Start, in.Start)
	set(&e.End, in.End)
	set(&e.TZID, in.TZID)
	set(&e.RRule, in.RRule)
	if in.AllDay != nil {
		e.AllDay = *in.AllDay
	}
	return normalizeEvent(e)
}

// normalizeEvent validates e and puts its times in stored form: dates for
// all-day events, RFC3339 UTC for timed ones.
func normalizeEvent(e *Event) error {
	if e.Title == "" {
		e.Title = "(no title)"
	}
	if len(e.Title) > 300 || len(e.Description) > 20000 || len(e.Location) > 500 || len(e.URL) > 2000 {
		return invalid("some of the event's text is too long")
	}
	if e.Color != "" && !colorRe.MatchString(e.Color) {
		return invalid("colour must look like #a1b2c3")
	}
	if e.TZID == "" {
		e.TZID = "UTC"
	}
	if _, err := time.LoadLocation(e.TZID); err != nil {
		return invalid("unknown time zone %q", e.TZID)
	}
	if e.AllDay {
		start, err := parseDateLoose(e.Start)
		if err != nil {
			return invalid("start must be a date")
		}
		end := start.AddDate(0, 0, 1)
		if e.End != "" {
			if end, err = parseDateLoose(e.End); err != nil {
				return invalid("end must be a date")
			}
		}
		if !end.After(start) {
			end = start.AddDate(0, 0, 1)
		}
		e.Start, e.End = start.Format(dateLayout), end.Format(dateLayout)
	} else {
		start, err := time.Parse(time.RFC3339, e.Start)
		if err != nil {
			return invalid("start must be a date and time")
		}
		end := start.Add(time.Hour)
		if e.End != "" {
			if end, err = time.Parse(time.RFC3339, e.End); err != nil {
				return invalid("end must be a date and time")
			}
		}
		if end.Before(start) {
			return invalid("the event ends before it starts")
		}
		e.Start, e.End = start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)
	}
	e.RRule = strings.TrimPrefix(strings.TrimSpace(e.RRule), "RRULE:")
	if e.RRule != "" {
		opt, err := rrule.StrToROption(e.RRule)
		if err != nil {
			return invalid("the repeat rule isn't valid")
		}
		// More often than daily isn't a calendar event, and would make every
		// query expand a huge series.
		if opt.Freq == rrule.HOURLY || opt.Freq == rrule.MINUTELY || opt.Freq == rrule.SECONDLY {
			return invalid("events can repeat at most daily")
		}
	}
	return nil
}

// parseDateLoose takes a date, or the date part of a timestamp.
func parseDateLoose(s string) (time.Time, error) {
	if len(s) > 10 {
		s = s[:10]
	}
	return time.Parse(dateLayout, s)
}

func (s *Service) insertEvent(ctx context.Context, q execer, e *Event, userID string) error {
	exdates, _ := json.Marshal(e.ExDates)
	now := stamp(s.now())
	_, err := q.ExecContext(ctx, `INSERT INTO events (id, calendar_id, uid, recurrence_id, title, description, location, url,
		all_day, start, end, tzid, rrule, exdates, cancelled, source, color, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.CalendarID, e.UID, e.RecurrenceID, e.Title, e.Description, e.Location, e.URL,
		e.AllDay, e.Start, e.End, e.TZID, e.RRule, string(exdates), e.Cancelled, e.Source, e.Color, userID, userID, now, now)
	return err
}

func (s *Service) saveEvent(ctx context.Context, q execer, e *Event, userID string) error {
	exdates, _ := json.Marshal(e.ExDates)
	_, err := q.ExecContext(ctx, `UPDATE events SET title = ?, description = ?, location = ?, url = ?, all_day = ?,
		start = ?, end = ?, tzid = ?, rrule = ?, exdates = ?, cancelled = ?, color = ?, updated_by = ?, updated_at = ?
		WHERE id = ?`, e.Title, e.Description, e.Location, e.URL, e.AllDay, e.Start, e.End, e.TZID, e.RRule,
		string(exdates), e.Cancelled, e.Color, userID, stamp(s.now()), e.ID)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Service) CreateEvent(ctx context.Context, calendarID, userID string, in EventInput) (*Event, error) {
	e := &Event{ID: uuid.NewString(), CalendarID: calendarID, UID: uuid.NewString() + "@supercalendar", Source: "manual", ExDates: []string{}}
	if err := in.apply(e); err != nil {
		return nil, err
	}
	if err := s.insertEvent(ctx, s.db, e, userID); err != nil {
		return nil, err
	}
	return s.GetEvent(ctx, calendarID, e.ID)
}

// UpdateEvent edits an event. With occurrence set (the original start of one
// occurrence of a series), only that occurrence changes: an override row is
// written, as ICS does with RECURRENCE-ID. Otherwise the whole series (or the
// override, if eventID is one) changes.
func (s *Service) UpdateEvent(ctx context.Context, calendarID, eventID, userID, occurrence string, in EventInput) (*Event, error) {
	e, err := s.GetEvent(ctx, calendarID, eventID)
	if err != nil {
		return nil, err
	}
	if stale(in.BaseUpdatedAt, e.UpdatedAt) {
		return nil, &ConflictError{Current: e, By: e.UpdatedBy}
	}
	if occurrence == "" || e.RRule == "" {
		before := *e
		if err := in.apply(e); err != nil {
			return nil, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		if before.RRule != "" && e.RecurrenceID == "" {
			if err := s.shiftExceptions(ctx, tx, &before, e); err != nil {
				return nil, err
			}
		}
		if err := s.saveEvent(ctx, tx, e, userID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetEvent(ctx, calendarID, eventID)
	}
	o, err := s.override(ctx, e, occurrence)
	if err != nil {
		return nil, err
	}
	if err := in.apply(o); err != nil {
		return nil, err
	}
	o.RRule = "" // an override is one occurrence
	if o.ID == "" {
		o.ID = uuid.NewString()
		err = s.insertEvent(ctx, s.db, o, userID)
	} else {
		err = s.saveEvent(ctx, s.db, o, userID)
	}
	if err != nil {
		return nil, err
	}
	return s.GetEvent(ctx, calendarID, o.ID)
}

// override returns the existing override of series at occurrence, or a new
// one (ID "") copied from the series and moved to that occurrence.
func (s *Service) override(ctx context.Context, series *Event, occurrence string) (*Event, error) {
	rid, err := normalizeOccurrence(series, occurrence)
	if err != nil {
		return nil, err
	}
	existing, err := s.queryEvents(ctx, `WHERE e.calendar_id = ? AND e.uid = ? AND e.recurrence_id = ?`, series.CalendarID, series.UID, rid)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	o := *series
	o.ID, o.RecurrenceID, o.RRule, o.ExDates = "", rid, "", []string{}
	if series.AllDay {
		st, _ := time.Parse(dateLayout, series.Start)
		en, _ := time.Parse(dateLayout, series.End)
		at, _ := time.Parse(dateLayout, rid)
		o.Start, o.End = rid, at.Add(en.Sub(st)).Format(dateLayout)
	} else {
		st, _ := time.Parse(time.RFC3339, series.Start)
		en, _ := time.Parse(time.RFC3339, series.End)
		at, _ := time.Parse(time.RFC3339, rid)
		o.Start, o.End = rid, at.Add(en.Sub(st)).UTC().Format(time.RFC3339)
	}
	return &o, nil
}

func normalizeOccurrence(series *Event, occ string) (string, error) {
	if series.AllDay {
		t, err := parseDateLoose(occ)
		if err != nil {
			return "", invalid("occurrence must be a date")
		}
		return t.Format(dateLayout), nil
	}
	t, err := time.Parse(time.RFC3339, occ)
	if err != nil {
		return "", invalid("occurrence must be a date and time")
	}
	return t.UTC().Format(time.RFC3339), nil
}

// DeleteEvent deletes a whole series (with its overrides), or with
// occurrence set just that one occurrence.
func (s *Service) DeleteEvent(ctx context.Context, calendarID, eventID, userID, occurrence string) error {
	e, err := s.GetEvent(ctx, calendarID, eventID)
	if err != nil {
		return err
	}
	if occurrence != "" && e.RRule != "" {
		rid, err := normalizeOccurrence(e, occurrence)
		if err != nil {
			return err
		}
		e.ExDates = append(e.ExDates, rid)
		if _, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE calendar_id = ? AND uid = ? AND recurrence_id = ?`, calendarID, e.UID, rid); err != nil {
			return err
		}
		return s.saveEvent(ctx, s.db, e, userID)
	}
	if e.RecurrenceID != "" {
		// Deleting an override: cancel that occurrence so the series doesn't
		// put it back.
		e.Cancelled = true
		return s.saveEvent(ctx, s.db, e, userID)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM events WHERE calendar_id = ? AND uid = ?`, calendarID, e.UID)
	return err
}

// Occurrence is one showing of an event in a range.
type Occurrence struct {
	EventID    string `json:"eventId"`
	CalendarID string `json:"calendarId"`
	// The series' original start of this occurrence ("" if not recurring):
	// pass it back as ?occurrence= to edit just this one.
	Occurrence string `json:"occurrence"`
	Recurring  bool   `json:"recurring"`
	Title      string `json:"title"`
	Location   string `json:"location"`
	Color      string `json:"color"` // the event's own, or ""
	AllDay     bool   `json:"allDay"`
	Start      string `json:"start"`
	End        string `json:"end"`
}

// Occurrences expands every event in the given calendars over r.
func (s *Service) Occurrences(ctx context.Context, calendarIDs []string, r Range) ([]Occurrence, error) {
	if len(calendarIDs) == 0 {
		return []Occurrence{}, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(calendarIDs)), ",")
	args := make([]any, 0, len(calendarIDs)+2)
	for _, id := range calendarIDs {
		args = append(args, id)
	}
	// Coarse prefilter by the stored strings, which sort as dates; recurring
	// events are always candidates. Dates widen by a day either side for zones.
	lo := r.From.AddDate(0, 0, -1).UTC().Format(dateLayout)
	hi := r.To.AddDate(0, 0, 1).UTC().Format(dateLayout)
	args = append(args, hi, lo)
	// Overrides are always loaded too: one moved here from outside the range
	// still has to hide the occurrence it replaced.
	events, err := s.queryEvents(ctx, `WHERE e.calendar_id IN (`+ph+`)
		AND (e.rrule != '' OR e.recurrence_id != '' OR (e.start < ? AND e.end >= ?))`, args...)
	if err != nil {
		return nil, err
	}

	overrides := map[string]bool{} // uid|recurrence_id
	for _, e := range events {
		if e.RecurrenceID != "" {
			overrides[e.UID+"|"+e.RecurrenceID] = true
		}
	}

	out := []Occurrence{}
	for _, e := range events {
		switch {
		case e.RecurrenceID != "":
			if !e.Cancelled && overlaps(e.AllDay, e.Start, e.End, r) {
				out = append(out, occ(e, e.ID, e.RecurrenceID, true, e.Start, e.End))
			}
		case e.RRule == "":
			if overlaps(e.AllDay, e.Start, e.End, r) {
				out = append(out, occ(e, e.ID, "", false, e.Start, e.End))
			}
		default:
			out = append(out, expand(e, r, overrides)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, nil
}

func occ(e *Event, id, occurrence string, recurring bool, start, end string) Occurrence {
	return Occurrence{EventID: id, CalendarID: e.CalendarID, Occurrence: occurrence, Recurring: recurring,
		Title: e.Title, Location: e.Location, Color: e.Color, AllDay: e.AllDay, Start: start, End: end}
}

func overlaps(allDay bool, start, end string, r Range) bool {
	if allDay {
		return start < r.ToDate && end > r.FromDate
	}
	st, err1 := time.Parse(time.RFC3339, start)
	en, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return false
	}
	if en.Equal(st) {
		return !st.Before(r.From) && st.Before(r.To)
	}
	return st.Before(r.To) && en.After(r.From)
}

// expand lists a series' occurrences that overlap r, minus exdates and the
// ones replaced by overrides. Timed series repeat in their own zone, so a
// 9:00 meeting stays at 9:00 across DST.
func expand(e *Event, r Range, overrides map[string]bool) []Occurrence {
	opt, err := rrule.StrToROption(e.RRule)
	if err != nil {
		return nil
	}
	var start, end time.Time
	var after, before time.Time
	if e.AllDay {
		start, _ = time.Parse(dateLayout, e.Start)
		end, _ = time.Parse(dateLayout, e.End)
		from, _ := time.Parse(dateLayout, r.FromDate)
		to, _ := time.Parse(dateLayout, r.ToDate)
		after, before = from.Add(-end.Sub(start)), to
	} else {
		loc, err := time.LoadLocation(e.TZID)
		if err != nil {
			loc = time.UTC
		}
		s, _ := time.Parse(time.RFC3339, e.Start)
		en, _ := time.Parse(time.RFC3339, e.End)
		start, end = s.In(loc), en.In(loc)
		after, before = r.From.Add(-end.Sub(start)), r.To
	}
	opt.Dtstart = start
	if !opt.Until.IsZero() && e.AllDay {
		// An all-day UNTIL is a date; make it cover that whole day.
		opt.Until = opt.Until.Add(24*time.Hour - time.Second)
	}
	rule, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil
	}
	ex := map[string]bool{}
	for _, d := range e.ExDates {
		ex[d] = true
	}
	dur := end.Sub(start)
	var out []Occurrence
	next := rule.Iterator()
	for steps := 0; steps < maxSteps && len(out) < maxOccurrences; steps++ {
		t, ok := next()
		if !ok || !t.Before(before) {
			break
		}
		if t.Before(after) {
			continue
		}
		var key, st, en string
		if e.AllDay {
			key, st, en = t.Format(dateLayout), t.Format(dateLayout), t.Add(dur).Format(dateLayout)
		} else {
			key = t.UTC().Format(time.RFC3339)
			st, en = key, t.Add(dur).UTC().Format(time.RFC3339)
		}
		if ex[key] || overrides[e.UID+"|"+key] {
			continue
		}
		if overlaps(e.AllDay, st, en, r) {
			out = append(out, occ(e, e.ID, key, true, st, en))
		}
	}
	return out
}

// shiftExceptions keeps a series' exceptions on the occurrences they belong
// to when the series' start moves (editing "all of them" from 9:00 to 10:00):
// its exdates and its overrides' recurrence ids are keyed by each
// occurrence's original start, so they move by the same wall-clock shift.
// Switching between all-day and timed can't be mapped, so the exceptions are
// dropped then.
func (s *Service) shiftExceptions(ctx context.Context, tx *sql.Tx, old, cur *Event) error {
	if old.Start == cur.Start && old.TZID == cur.TZID && old.AllDay == cur.AllDay {
		return nil
	}
	if old.AllDay != cur.AllDay {
		cur.ExDates = []string{}
		_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE calendar_id = ? AND uid = ? AND recurrence_id != ''`, cur.CalendarID, cur.UID)
		return err
	}
	shift := wallShift(old, cur)
	exdates := make([]string, 0, len(old.ExDates))
	for _, d := range old.ExDates {
		if n, ok := shift(d); ok {
			exdates = append(exdates, n)
		}
	}
	cur.ExDates = exdates

	rows, err := tx.QueryContext(ctx, `SELECT id, recurrence_id FROM events WHERE calendar_id = ? AND uid = ? AND recurrence_id != ''`, cur.CalendarID, cur.UID)
	if err != nil {
		return err
	}
	type move struct{ id, rid string }
	var moves []move
	for rows.Next() {
		var m move
		if err := rows.Scan(&m.id, &m.rid); err != nil {
			rows.Close()
			return err
		}
		moves = append(moves, m)
	}
	rows.Close()
	for _, m := range moves {
		n, ok := shift(m.rid)
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET recurrence_id = ? WHERE id = ?`, n, m.id); err != nil {
			return err
		}
	}
	return nil
}

// wallShift returns a function moving an occurrence key of old to the matching
// one of cur: by the change in local date and clock time, in each one's zone.
func wallShift(old, cur *Event) func(string) (string, bool) {
	if old.AllDay {
		o, _ := time.Parse(dateLayout, old.Start)
		n, _ := time.Parse(dateLayout, cur.Start)
		days := int(n.Sub(o).Hours() / 24)
		return func(k string) (string, bool) {
			t, err := time.Parse(dateLayout, k)
			if err != nil {
				return "", false
			}
			return t.AddDate(0, 0, days).Format(dateLayout), true
		}
	}
	oldLoc, err := time.LoadLocation(old.TZID)
	if err != nil {
		oldLoc = time.UTC
	}
	curLoc, err := time.LoadLocation(cur.TZID)
	if err != nil {
		curLoc = time.UTC
	}
	o, _ := time.Parse(time.RFC3339, old.Start)
	n, _ := time.Parse(time.RFC3339, cur.Start)
	o, n = o.In(oldLoc), n.In(curLoc)
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	dayShift := int(day(n).Sub(day(o)).Hours() / 24)
	clockShift := clock(n) - clock(o)
	return func(k string) (string, bool) {
		t, err := time.Parse(time.RFC3339, k)
		if err != nil {
			return "", false
		}
		l := t.In(oldLoc)
		// time.Date normalises the seconds into wall-clock time in curLoc, so
		// the hour holds across DST.
		moved := time.Date(l.Year(), l.Month(), l.Day()+dayShift, 0, 0, int((clock(l)+clockShift)/time.Second), 0, curLoc)
		return moved.UTC().Format(time.RFC3339), true
	}
}

func clock(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second
}
