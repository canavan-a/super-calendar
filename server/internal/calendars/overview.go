package calendars

import (
	"context"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/cycle"
)

// Item is one thing drawn on the Overview, whatever calendar kind it came
// from. Timed items have Start/End instants; all-day and dated ones have
// Start/End dates (End exclusive).
type Item struct {
	CalendarID string `json:"calendarId"`
	Kind       string `json:"kind"`
	// Which switchable layer of its kind it belongs to:
	//   general:  event
	//   training: planned, done, skipped
	//   cycle:    period (a logged day), predicted, fertile
	Layer      string `json:"layer"`
	AllDay     bool   `json:"allDay"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Title      string `json:"title"`
	Color      string `json:"color"`
	RefID      string `json:"refId,omitempty"`      // event or session id
	Occurrence string `json:"occurrence,omitempty"` // for a recurring event
	Flow       string `json:"flow,omitempty"`       // cycle: the logged flow
	Time       string `json:"time,omitempty"`       // training: HH:MM
}

// adapter turns one kind's data in a range into Overview items. A new kind
// only needs a new adapter.
type adapter func(ctx context.Context, s *Service, cals []*Calendar, r Range) ([]Item, error)

var adapters = map[string]adapter{
	General:  generalItems,
	Training: trainingItems,
	Cycle:    cycleItems,
}

// Overview overlays every calendar userID has switched on for the Overview.
// Type and layer switches are the app's to apply (they're in user settings),
// so flipping one needs no new request.
func (s *Service) Overview(ctx context.Context, userID string, r Range) ([]Item, error) {
	all, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	byKind := map[string][]*Calendar{}
	for _, c := range all {
		if c.InOverview {
			byKind[c.Kind] = append(byKind[c.Kind], c)
		}
	}
	out := []Item{}
	for kind, cals := range byKind {
		items, err := adapters[kind](ctx, s, cals, r)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func ids(cals []*Calendar) ([]string, map[string]*Calendar) {
	out := make([]string, 0, len(cals))
	m := map[string]*Calendar{}
	for _, c := range cals {
		out = append(out, c.ID)
		m[c.ID] = c
	}
	return out, m
}

func generalItems(ctx context.Context, s *Service, cals []*Calendar, r Range) ([]Item, error) {
	idList, byID := ids(cals)
	occs, err := s.Occurrences(ctx, idList, r)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(occs))
	for _, o := range occs {
		out = append(out, Item{CalendarID: o.CalendarID, Kind: General, Layer: "event", AllDay: o.AllDay,
			Start: o.Start, End: o.End, Title: o.Title, Color: byID[o.CalendarID].DisplayColor(),
			RefID: o.EventID, Occurrence: o.Occurrence})
	}
	return out, nil
}

func trainingItems(ctx context.Context, s *Service, cals []*Calendar, r Range) ([]Item, error) {
	idList, byID := ids(cals)
	sessions, err := s.Sessions(ctx, idList, r.FromDate, r.ToDate)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(sessions))
	for _, t := range sessions {
		d, _ := time.Parse(dateLayout, t.Date)
		out = append(out, Item{CalendarID: t.CalendarID, Kind: Training, Layer: t.Status, AllDay: true,
			Start: t.Date, End: d.AddDate(0, 0, 1).Format(dateLayout), Title: t.Title,
			Color: byID[t.CalendarID].DisplayColor(), RefID: t.ID, Time: t.Time})
	}
	return out, nil
}

func cycleItems(ctx context.Context, s *Service, cals []*Calendar, r Range) ([]Item, error) {
	var out []Item
	day := func(t time.Time) string { return t.Format(dateLayout) }
	inRange := func(start, end string) bool { return start < r.ToDate && end > r.FromDate }
	for _, c := range cals {
		color := c.DisplayColor()
		// The whole history once: the summary needs it all, and the range's
		// days are picked out of it.
		logged, err := s.CycleDays(ctx, c.ID, "", "")
		if err != nil {
			return nil, err
		}
		for _, d := range logged {
			if d.Flow == cycle.None || d.Date < r.FromDate || d.Date >= r.ToDate {
				continue
			}
			t, _ := time.Parse(dateLayout, d.Date)
			out = append(out, Item{CalendarID: c.ID, Kind: Cycle, Layer: "period", AllDay: true,
				Start: d.Date, End: day(t.AddDate(0, 0, 1)), Title: c.Name, Color: color, Flow: d.Flow})
		}
		sum := s.summarize(logged, r.Loc)
		for _, p := range sum.Predictions {
			start, _ := time.Parse(dateLayout, p.Start)
			end, _ := time.Parse(dateLayout, p.End)
			// A late period's predicted days are in the past; only draw what's
			// still to come.
			if end.Format(dateLayout) < sum.Today {
				continue
			}
			if s := start.Format(dateLayout); s < sum.Today {
				start, _ = time.Parse(dateLayout, sum.Today)
			}
			if inRange(day(start), day(end.AddDate(0, 0, 1))) {
				out = append(out, Item{CalendarID: c.ID, Kind: Cycle, Layer: "predicted", AllDay: true,
					Start: day(start), End: day(end.AddDate(0, 0, 1)), Title: c.Name + " (predicted)", Color: color})
			}
			fs, _ := time.Parse(dateLayout, p.FertileStart)
			fe, _ := time.Parse(dateLayout, p.FertileEnd)
			if inRange(day(fs), day(fe.AddDate(0, 0, 1))) && day(fe) >= sum.Today {
				out = append(out, Item{CalendarID: c.ID, Kind: Cycle, Layer: "fertile", AllDay: true,
					Start: day(fs), End: day(fe.AddDate(0, 0, 1)), Title: c.Name + " (fertile window estimate)", Color: color})
			}
		}
	}
	return out, nil
}
