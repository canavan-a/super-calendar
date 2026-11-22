package calendars

import (
	"context"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/cycle"
)

type CycleDay struct {
	Date      string `json:"date"`
	Flow      string `json:"flow"`
	Notes     string `json:"notes"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Service) CycleDays(ctx context.Context, calendarID, from, to string) ([]CycleDay, error) {
	q := `SELECT d.date, d.flow, d.notes, COALESCE(u.display_name, ''), d.updated_at
		FROM cycle_days d LEFT JOIN users u ON u.id = d.updated_by WHERE d.calendar_id = ?`
	args := []any{calendarID}
	if from != "" {
		q += ` AND d.date >= ?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND d.date < ?`
		args = append(args, to)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY d.date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CycleDay{}
	for rows.Next() {
		var d CycleDay
		if err := rows.Scan(&d.Date, &d.Flow, &d.Notes, &d.UpdatedBy, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutCycleDay logs a day. Flow "none" with no notes clears it back to blank.
func (s *Service) PutCycleDay(ctx context.Context, calendarID, userID, date, flow, notes string) error {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return invalid("date must be a date")
	}
	if flow == "" {
		flow = cycle.None
	}
	if !cycle.ValidFlow(flow) {
		return invalid("flow is none, spotting, light, medium or heavy")
	}
	if len(notes) > 2000 {
		return invalid("notes are too long")
	}
	if flow == cycle.None && notes == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM cycle_days WHERE calendar_id = ? AND date = ?`, calendarID, date)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO cycle_days (calendar_id, date, flow, notes, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (calendar_id, date) DO UPDATE SET flow = excluded.flow, notes = excluded.notes,
			updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		calendarID, date, flow, notes, userID, stamp(s.now()))
	return err
}

type PeriodJSON struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Length int    `json:"length"`
}

type CycleJSON struct {
	Start    string `json:"start"`
	Length   int    `json:"length"`
	Excluded bool   `json:"excluded"`
}

type PredictionJSON struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	RangeDays    int    `json:"rangeDays"`
	Ovulation    string `json:"ovulation"`
	FertileStart string `json:"fertileStart"`
	FertileEnd   string `json:"fertileEnd"`
}

// SpanJSON is a run of dates, both ends included.
type SpanJSON struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type CycleSummary struct {
	Today        string           `json:"today"`
	Periods      []PeriodJSON     `json:"periods"`
	Cycles       []CycleJSON      `json:"cycles"`
	Enough       bool             `json:"enough"`
	NeedPeriods  int              `json:"needPeriods"`
	MeanCycle    float64          `json:"meanCycle"`
	SDCycle      float64          `json:"sdCycle"`
	PeriodLength int              `json:"periodLength"`
	Irregular    bool             `json:"irregular"`
	Predictions  []PredictionJSON `json:"predictions"`
	LateDays     int              `json:"lateDays"`
	// The rest of a period under way, as expected (both ends included).
	Ongoing *SpanJSON `json:"ongoing"`
}

func f(t time.Time) string { return t.Format(dateLayout) }

// CycleSummary detects periods and predicts the next ones, as of today in loc.
func (s *Service) CycleSummary(ctx context.Context, calendarID string, loc *time.Location) (*CycleSummary, error) {
	logged, err := s.CycleDays(ctx, calendarID, "", "")
	if err != nil {
		return nil, err
	}
	return s.summarize(logged, loc), nil
}

// summarize is CycleSummary for days already loaded (the whole history).
func (s *Service) summarize(logged []CycleDay, loc *time.Location) *CycleSummary {
	days := make([]cycle.Day, 0, len(logged))
	for _, d := range logged {
		t, err := time.Parse(dateLayout, d.Date)
		if err == nil {
			days = append(days, cycle.Day{Date: t, Flow: d.Flow})
		}
	}
	today, _ := time.Parse(dateLayout, s.now().In(loc).Format(dateLayout))
	sum := cycle.Summarize(days, today)
	out := &CycleSummary{
		Today: f(today), Periods: []PeriodJSON{}, Cycles: []CycleJSON{}, Predictions: []PredictionJSON{},
		Enough: sum.Enough, NeedPeriods: sum.NeedPeriods, MeanCycle: round1(sum.MeanCycle), SDCycle: round1(sum.SDCycle),
		PeriodLength: sum.PeriodLength, Irregular: sum.Irregular, LateDays: sum.LateDays,
	}
	if sum.Ongoing != nil {
		out.Ongoing = &SpanJSON{Start: f(sum.Ongoing.Start), End: f(sum.Ongoing.End)}
	}
	for _, p := range sum.Periods {
		out.Periods = append(out.Periods, PeriodJSON{Start: f(p.Start), End: f(p.End), Length: p.Length})
	}
	for _, c := range sum.Cycles {
		out.Cycles = append(out.Cycles, CycleJSON{Start: f(c.Start), Length: c.Length, Excluded: c.Excluded})
	}
	for _, p := range sum.Predictions {
		out.Predictions = append(out.Predictions, PredictionJSON{Start: f(p.Start), End: f(p.End), RangeDays: p.RangeDays,
			Ovulation: f(p.Ovulation), FertileStart: f(p.FertileStart), FertileEnd: f(p.FertileEnd)})
	}
	return out
}

func round1(x float64) float64 { return float64(int(x*10+0.5)) / 10 }
