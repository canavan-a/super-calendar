package calendars

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Plan fields an activity type can ask for. A session's plan (and result) is
// a JSON object using these keys:
//
//	duration  -> "durationMin": number
//	distance  -> "distanceKm": number
//	intensity -> "intensity": "easy" | "moderate" | "hard" | "max"
//	exercises -> "exercises": [{"name", "sets", "reps", "weight"}]
var activityFields = map[string]bool{"duration": true, "distance": true, "intensity": true, "exercises": true}

var planKeys = map[string]bool{"durationMin": true, "distanceKm": true, "intensity": true, "exercises": true}

type ActivityType struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`
	Icon     string   `json:"icon"`
	Fields   []string `json:"fields"`
	Archived bool     `json:"archived"`
	Position int      `json:"position"`
}

// A new training calendar starts with a small library; rest and mobility are
// there so a plan can cover non-workout days too.
var defaultActivityTypes = []ActivityType{
	{Name: "Run", Color: "#e8590c", Icon: "run", Fields: []string{"duration", "distance", "intensity"}},
	{Name: "Ride", Color: "#2f9e44", Icon: "bike", Fields: []string{"duration", "distance", "intensity"}},
	{Name: "Swim", Color: "#1c7ed6", Icon: "swim", Fields: []string{"duration", "distance"}},
	{Name: "Strength", Color: "#6741d9", Icon: "lift", Fields: []string{"duration", "exercises"}},
	{Name: "Mobility", Color: "#c2255c", Icon: "stretch", Fields: []string{"duration"}},
	{Name: "Rest", Color: "#868e96", Icon: "rest", Fields: []string{}},
}

func seedActivityTypes(ctx context.Context, tx *sql.Tx, calendarID string) error {
	for i, t := range defaultActivityTypes {
		fields, _ := json.Marshal(t.Fields)
		if _, err := tx.ExecContext(ctx, `INSERT INTO activity_types (id, calendar_id, name, color, icon, fields, position)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), calendarID, t.Name, t.Color, t.Icon, string(fields), i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ActivityTypes(ctx context.Context, calendarID string) ([]ActivityType, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, color, icon, fields, archived, position
		FROM activity_types WHERE calendar_id = ? ORDER BY position, name`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityType{}
	for rows.Next() {
		var t ActivityType
		var fields string
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Icon, &fields, &t.Archived, &t.Position); err != nil {
			return nil, err
		}
		t.Fields = []string{}
		_ = json.Unmarshal([]byte(fields), &t.Fields)
		out = append(out, t)
	}
	return out, rows.Err()
}

type ActivityTypeInput struct {
	Name     *string   `json:"name"`
	Color    *string   `json:"color"`
	Icon     *string   `json:"icon"`
	Fields   *[]string `json:"fields"`
	Archived *bool     `json:"archived"`
	Position *int      `json:"position"`
}

func (in ActivityTypeInput) apply(t *ActivityType) error {
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if t.Name == "" || len(t.Name) > 60 {
		return invalid("give the activity a name of up to 60 characters")
	}
	if in.Color != nil {
		t.Color = *in.Color
	}
	if !colorRe.MatchString(t.Color) {
		return invalid("colour must look like #a1b2c3")
	}
	if in.Icon != nil {
		t.Icon = strings.TrimSpace(*in.Icon)
	}
	if in.Fields != nil {
		t.Fields = []string{}
		for _, f := range *in.Fields {
			if !activityFields[f] {
				return invalid("unknown activity field %q", f)
			}
			t.Fields = append(t.Fields, f)
		}
	}
	if t.Fields == nil {
		t.Fields = []string{}
	}
	if in.Archived != nil {
		t.Archived = *in.Archived
	}
	if in.Position != nil {
		t.Position = *in.Position
	}
	return nil
}

func (s *Service) CreateActivityType(ctx context.Context, calendarID string, in ActivityTypeInput) (*ActivityType, error) {
	t := ActivityType{ID: uuid.NewString(), Color: "#495057"}
	if err := in.apply(&t); err != nil {
		return nil, err
	}
	if in.Position == nil {
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM activity_types WHERE calendar_id = ?`, calendarID).Scan(&t.Position)
	}
	fields, _ := json.Marshal(t.Fields)
	_, err := s.db.ExecContext(ctx, `INSERT INTO activity_types (id, calendar_id, name, color, icon, fields, archived, position)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, calendarID, t.Name, t.Color, t.Icon, string(fields), t.Archived, t.Position)
	return &t, err
}

func (s *Service) UpdateActivityType(ctx context.Context, calendarID, typeID string, in ActivityTypeInput) (*ActivityType, error) {
	types, err := s.ActivityTypes(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		if t.ID != typeID {
			continue
		}
		if err := in.apply(&t); err != nil {
			return nil, err
		}
		fields, _ := json.Marshal(t.Fields)
		_, err := s.db.ExecContext(ctx, `UPDATE activity_types SET name = ?, color = ?, icon = ?, fields = ?, archived = ?, position = ?
			WHERE id = ? AND calendar_id = ?`, t.Name, t.Color, t.Icon, string(fields), t.Archived, t.Position, t.ID, calendarID)
		return &t, err
	}
	return nil, ErrNotFound
}

type Session struct {
	ID             string          `json:"id"`
	CalendarID     string          `json:"calendarId"`
	Date           string          `json:"date"`
	Time           string          `json:"time"`
	Position       int             `json:"position"`
	ActivityTypeID *string         `json:"activityTypeId"`
	Title          string          `json:"title"`
	Plan           json.RawMessage `json:"plan"`
	Notes          string          `json:"notes"`
	Status         string          `json:"status"`
	Result         json.RawMessage `json:"result"`
	UpdatedBy      string          `json:"updatedBy"`
	UpdatedAt      string          `json:"updatedAt"`
}

const sessionCols = `t.id, t.calendar_id, t.date, t.time, t.position, t.activity_type_id, t.title, t.plan, t.notes,
	t.status, t.result, COALESCE(u.display_name, ''), t.updated_at`

const sessionFrom = ` FROM training_sessions t LEFT JOIN users u ON u.id = t.updated_by `

func (s *Service) querySessions(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, where string, args ...any) ([]Session, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+sessionCols+sessionFrom+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var t Session
		var typeID sql.NullString
		var plan, result string
		if err := rows.Scan(&t.ID, &t.CalendarID, &t.Date, &t.Time, &t.Position, &typeID, &t.Title, &plan, &t.Notes,
			&t.Status, &result, &t.UpdatedBy, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if typeID.Valid {
			t.ActivityTypeID = &typeID.String
		}
		t.Plan, t.Result = json.RawMessage(plan), json.RawMessage(result)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Sessions lists the sessions in the given calendars dated in [from, to).
func (s *Service) Sessions(ctx context.Context, calendarIDs []string, from, to string) ([]Session, error) {
	if len(calendarIDs) == 0 {
		return []Session{}, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(calendarIDs)), ",")
	args := []any{}
	for _, id := range calendarIDs {
		args = append(args, id)
	}
	args = append(args, from, to)
	return s.querySessions(ctx, s.db, `WHERE t.calendar_id IN (`+ph+`) AND t.date >= ? AND t.date < ?
		ORDER BY t.date, t.position, t.time`, args...)
}

func (s *Service) GetSession(ctx context.Context, calendarID, sessionID string) (*Session, error) {
	out, err := s.querySessions(ctx, s.db, `WHERE t.calendar_id = ? AND t.id = ?`, calendarID, sessionID)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

type SessionInput struct {
	Date           *string          `json:"date"`
	Time           *string          `json:"time"`
	Position       *int             `json:"position"`
	ActivityTypeID *string          `json:"activityTypeId"` // "" clears it
	Title          *string          `json:"title"`
	Plan           *json.RawMessage `json:"plan"`
	Notes          *string          `json:"notes"`
	Status         *string          `json:"status"`
	Result         *json.RawMessage `json:"result"`
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func validPlan(raw json.RawMessage) error {
	if len(raw) > 16<<10 {
		return invalid("the plan is too large")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return invalid("the plan must be an object")
	}
	for k := range m {
		if !planKeys[k] && k != "notes" {
			return invalid("unknown plan field %q", k)
		}
	}
	return nil
}

func (s *Service) applySession(ctx context.Context, t *Session, in SessionInput) error {
	if in.Date != nil {
		if _, err := time.Parse(dateLayout, *in.Date); err != nil {
			return invalid("date must be a date")
		}
		t.Date = *in.Date
	}
	if in.Time != nil {
		if *in.Time != "" && !hhmm.MatchString(*in.Time) {
			return invalid("time must look like 07:30")
		}
		t.Time = *in.Time
	}
	if in.Position != nil {
		t.Position = *in.Position
	}
	if in.ActivityTypeID != nil {
		if *in.ActivityTypeID == "" {
			t.ActivityTypeID = nil
		} else {
			var n int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_types WHERE id = ? AND calendar_id = ?`,
				*in.ActivityTypeID, t.CalendarID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return invalid("that activity isn't in this calendar")
			}
			id := *in.ActivityTypeID
			t.ActivityTypeID = &id
		}
	}
	if in.Title != nil {
		t.Title = strings.TrimSpace(*in.Title)
	}
	if t.Title == "" && t.ActivityTypeID != nil {
		_ = s.db.QueryRowContext(ctx, `SELECT name FROM activity_types WHERE id = ?`, *t.ActivityTypeID).Scan(&t.Title)
	}
	if t.Title == "" || len(t.Title) > 200 {
		return invalid("give the session a title of up to 200 characters")
	}
	if in.Plan != nil {
		if err := validPlan(*in.Plan); err != nil {
			return err
		}
		t.Plan = *in.Plan
	}
	if in.Notes != nil {
		t.Notes = *in.Notes
	}
	if in.Status != nil {
		switch *in.Status {
		case "planned", "done", "skipped":
			t.Status = *in.Status
		default:
			return invalid("status is planned, done or skipped")
		}
	}
	if in.Result != nil {
		if err := validPlan(*in.Result); err != nil {
			return err
		}
		t.Result = *in.Result
	}
	if len(t.Plan) == 0 {
		t.Plan = json.RawMessage(`{}`)
	}
	if len(t.Result) == 0 {
		t.Result = json.RawMessage(`{}`)
	}
	return nil
}

func (s *Service) insertSession(ctx context.Context, q execer, t *Session, userID string) error {
	now := stamp(s.now())
	_, err := q.ExecContext(ctx, `INSERT INTO training_sessions (id, calendar_id, date, time, position, activity_type_id,
		title, plan, notes, status, result, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, t.CalendarID, t.Date, t.Time, t.Position, t.ActivityTypeID,
		t.Title, string(t.Plan), t.Notes, t.Status, string(t.Result), userID, userID, now, now)
	return err
}

func (s *Service) nextPosition(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, calendarID, date string) int {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM training_sessions WHERE calendar_id = ? AND date = ?`,
		calendarID, date).Scan(&n)
	return n
}

func (s *Service) CreateSession(ctx context.Context, calendarID, userID string, in SessionInput) (*Session, error) {
	t := &Session{ID: uuid.NewString(), CalendarID: calendarID, Status: "planned"}
	if in.Date == nil {
		return nil, invalid("date must be a date")
	}
	if err := s.applySession(ctx, t, in); err != nil {
		return nil, err
	}
	if in.Position == nil {
		t.Position = s.nextPosition(ctx, s.db, calendarID, t.Date)
	}
	if err := s.insertSession(ctx, s.db, t, userID); err != nil {
		return nil, err
	}
	return s.GetSession(ctx, calendarID, t.ID)
}

func (s *Service) UpdateSession(ctx context.Context, calendarID, sessionID, userID string, in SessionInput) (*Session, error) {
	t, err := s.GetSession(ctx, calendarID, sessionID)
	if err != nil {
		return nil, err
	}
	movedDay := in.Date != nil && *in.Date != t.Date
	if err := s.applySession(ctx, t, in); err != nil {
		return nil, err
	}
	if movedDay && in.Position == nil {
		t.Position = s.nextPosition(ctx, s.db, calendarID, t.Date)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE training_sessions SET date = ?, time = ?, position = ?, activity_type_id = ?,
		title = ?, plan = ?, notes = ?, status = ?, result = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
		t.Date, t.Time, t.Position, t.ActivityTypeID, t.Title, string(t.Plan), t.Notes, t.Status, string(t.Result),
		userID, stamp(s.now()), t.ID)
	if err != nil {
		return nil, err
	}
	return s.GetSession(ctx, calendarID, sessionID)
}

func (s *Service) DeleteSession(ctx context.Context, calendarID, sessionID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM training_sessions WHERE calendar_id = ? AND id = ?`, calendarID, sessionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type CopyRequest struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	Days int      `json:"days"`
	Mode string   `json:"mode"` // merge | replace
}

// CopyResult says what a copy changed, so the app can undo it with Restore.
type CopyResult struct {
	Created []string  `json:"created"`
	Removed []Session `json:"removed"`
}

// Copy copies the sessions in [from, from+days) onto each target, keeping
// each one's offset, time and order, as planned with no result. "replace"
// clears each target span first; "merge" adds after what's there.
func (s *Service) Copy(ctx context.Context, calendarID, userID string, req CopyRequest) (*CopyResult, error) {
	from, err := time.Parse(dateLayout, req.From)
	if err != nil {
		return nil, invalid("from must be a date")
	}
	if req.Days < 1 || req.Days > 28 {
		return nil, invalid("copy between 1 and 28 days")
	}
	if len(req.To) == 0 || len(req.To) > 52 {
		return nil, invalid("pick between 1 and 52 targets")
	}
	if req.Mode == "" {
		req.Mode = "merge"
	}
	if req.Mode != "merge" && req.Mode != "replace" {
		return nil, invalid("mode is merge or replace")
	}
	end := from.AddDate(0, 0, req.Days).Format(dateLayout)
	var targets []time.Time
	for _, t := range req.To {
		d, err := time.Parse(dateLayout, t)
		if err != nil {
			return nil, invalid("targets must be dates")
		}
		if d.Before(from.AddDate(0, 0, req.Days)) && d.AddDate(0, 0, req.Days).After(from) {
			return nil, invalid("a target overlaps the days being copied")
		}
		targets = append(targets, d)
	}
	// Targets overlapping each other would have a later "replace" delete an
	// earlier target's copies, which undo would then put back.
	sorted := append([]time.Time(nil), targets...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Before(sorted[i-1].AddDate(0, 0, req.Days)) {
			return nil, invalid("targets overlap each other")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	src, err := s.querySessions(ctx, tx, `WHERE t.calendar_id = ? AND t.date >= ? AND t.date < ? ORDER BY t.date, t.position`,
		calendarID, req.From, end)
	if err != nil {
		return nil, err
	}
	res := &CopyResult{Created: []string{}, Removed: []Session{}}
	for _, target := range targets {
		tStart, tEnd := target.Format(dateLayout), target.AddDate(0, 0, req.Days).Format(dateLayout)
		if req.Mode == "replace" {
			gone, err := s.querySessions(ctx, tx, `WHERE t.calendar_id = ? AND t.date >= ? AND t.date < ?`, calendarID, tStart, tEnd)
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM training_sessions WHERE calendar_id = ? AND date >= ? AND date < ?`,
				calendarID, tStart, tEnd); err != nil {
				return nil, err
			}
			res.Removed = append(res.Removed, gone...)
		}
		base := map[string]int{} // date -> first free position
		for _, srcSession := range src {
			d, _ := time.Parse(dateLayout, srcSession.Date)
			date := target.AddDate(0, 0, int(d.Sub(from).Hours()/24)).Format(dateLayout)
			if _, ok := base[date]; !ok {
				base[date] = s.nextPosition(ctx, tx, calendarID, date)
			}
			c := srcSession
			c.ID, c.Date, c.Status, c.Result = uuid.NewString(), date, "planned", json.RawMessage(`{}`)
			c.Position = base[date]
			base[date]++
			if err := s.insertSession(ctx, tx, &c, userID); err != nil {
				return nil, err
			}
			res.Created = append(res.Created, c.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

type RestoreRequest struct {
	Delete  []string  `json:"delete"`
	Restore []Session `json:"restore"`
}

// Restore undoes a copy: it deletes what was created and puts back what was
// removed, as it was.
func (s *Service) Restore(ctx context.Context, calendarID, userID string, req RestoreRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range req.Delete {
		if _, err := tx.ExecContext(ctx, `DELETE FROM training_sessions WHERE calendar_id = ? AND id = ?`, calendarID, id); err != nil {
			return err
		}
	}
	for _, t := range req.Restore {
		t.CalendarID = calendarID
		if _, err := time.Parse(dateLayout, t.Date); err != nil || strings.TrimSpace(t.Title) == "" {
			return invalid("couldn't restore a session")
		}
		if t.Status != "planned" && t.Status != "done" && t.Status != "skipped" {
			t.Status = "planned"
		}
		if t.ActivityTypeID != nil {
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_types WHERE id = ? AND calendar_id = ?`, *t.ActivityTypeID, calendarID).Scan(&n)
			if n == 0 {
				t.ActivityTypeID = nil
			}
		}
		if err := validPlan(t.Plan); err != nil {
			return err
		}
		if err := validPlan(t.Result); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM training_sessions WHERE id = ? AND calendar_id = ?`, t.ID, calendarID); err != nil {
			return err
		}
		if err := s.insertSession(ctx, tx, &t, userID); err != nil {
			if strings.Contains(err.Error(), "constraint") {
				return invalid("couldn't restore a session")
			}
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
