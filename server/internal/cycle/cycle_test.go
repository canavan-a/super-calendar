package cycle

import (
	"math/rand"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ds(t time.Time) string { return t.Format(DateLayout) }

// period logs n days of flow from start: heavy, then medium, then light.
func period(start string, n int) []Day {
	var out []Day
	for i := 0; i < n; i++ {
		f := Light
		switch {
		case i == 0:
			f = Heavy
		case i < n/2:
			f = Medium
		}
		out = append(out, Day{Date: addDays(d(start), i), Flow: f})
	}
	return out
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name   string
		days   []Day
		starts []string
		lens   []int
	}{
		{"empty", nil, nil, nil},
		{"one run", period("2026-01-01", 5), []string{"2026-01-01"}, []int{5}},
		{
			"one-day gap stays one period",
			[]Day{{d("2026-01-01"), Heavy}, {d("2026-01-03"), Light}},
			[]string{"2026-01-01"}, []int{3},
		},
		{
			"two-day gap splits",
			[]Day{{d("2026-01-01"), Heavy}, {d("2026-01-04"), Light}},
			[]string{"2026-01-01", "2026-01-04"}, []int{1, 1},
		},
		{
			"spotting bridges inside a period",
			[]Day{{d("2026-01-01"), Heavy}, {d("2026-01-02"), Spotting}, {d("2026-01-03"), Spotting}, {d("2026-01-04"), Light}},
			[]string{"2026-01-01"}, []int{4},
		},
		{
			"spotting alone never starts a period",
			[]Day{{d("2026-01-01"), Spotting}, {d("2026-01-02"), Spotting}, {d("2026-01-03"), Medium}, {d("2026-01-05"), Spotting}},
			[]string{"2026-01-03"}, []int{1},
		},
		{
			"none days count as a gap",
			[]Day{{d("2026-01-01"), Heavy}, {d("2026-01-02"), None}, {d("2026-01-03"), None}, {d("2026-01-04"), Light}},
			[]string{"2026-01-01", "2026-01-04"}, []int{1, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.days)
			if len(got) != len(tt.starts) {
				t.Fatalf("got %d periods %+v, want %d", len(got), got, len(tt.starts))
			}
			for i, p := range got {
				if ds(p.Start) != tt.starts[i] || p.Length != tt.lens[i] {
					t.Errorf("period %d = %s len %d, want %s len %d", i, ds(p.Start), p.Length, tt.starts[i], tt.lens[i])
				}
			}
		})
	}
}

func history(starts ...string) []Day {
	var out []Day
	for _, s := range starts {
		out = append(out, period(s, 5)...)
	}
	return out
}

func TestNotEnoughData(t *testing.T) {
	s := Summarize(history("2026-01-01", "2026-01-29"), d("2026-02-10"))
	if s.Enough || s.NeedPeriods != 1 || len(s.Predictions) != 0 {
		t.Fatalf("want 1 more period needed, got %+v", s)
	}
	if s := Summarize(nil, d("2026-02-10")); s.NeedPeriods != 2 {
		t.Fatalf("empty: need %d", s.NeedPeriods)
	}
}

func TestRegularPrediction(t *testing.T) {
	// 28-day cycles.
	s := Summarize(history("2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"), d("2026-04-05"))
	if !s.Enough || s.MeanCycle != 28 || s.Irregular {
		t.Fatalf("summary %+v", s)
	}
	p := s.Predictions
	if len(p) != 3 || ds(p[0].Start) != "2026-04-23" || ds(p[1].Start) != "2026-05-21" {
		t.Fatalf("predictions %v %v", ds(p[0].Start), ds(p[1].Start))
	}
	if p[0].RangeDays != 1 || ds(p[0].End) != "2026-04-27" || s.PeriodLength != 5 {
		t.Fatalf("range %d end %s len %d", p[0].RangeDays, ds(p[0].End), s.PeriodLength)
	}
	if ds(p[0].Ovulation) != "2026-04-09" || ds(p[0].FertileStart) != "2026-04-04" || ds(p[0].FertileEnd) != "2026-04-10" {
		t.Fatalf("fertile %s %s %s", ds(p[0].Ovulation), ds(p[0].FertileStart), ds(p[0].FertileEnd))
	}
	if p[2].RangeDays < p[0].RangeDays {
		t.Fatalf("range should widen: %d then %d", p[0].RangeDays, p[2].RangeDays)
	}
	if s.LateDays != 0 {
		t.Fatalf("late %d", s.LateDays)
	}
}

func TestExcludesImplausibleCycles(t *testing.T) {
	// A missed log makes a 70-day gap, which is dropped.
	s := Summarize(history("2026-01-01", "2026-01-31", "2026-04-11", "2026-05-11", "2026-06-10"), d("2026-06-20"))
	if !s.Cycles[1].Excluded || s.MeanCycle != 30 {
		t.Fatalf("cycles %+v mean %v", s.Cycles, s.MeanCycle)
	}
}

func TestRecentCyclesWeighMore(t *testing.T) {
	// Cycles lengthen from 26 to 32: the weighted mean leans to the recent end.
	s := Summarize(history("2026-01-01", "2026-01-27", "2026-02-22", "2026-03-24", "2026-04-25"), d("2026-04-30"))
	if s.MeanCycle <= 29 {
		t.Fatalf("mean %v, want above the plain mean of 28.5", s.MeanCycle)
	}
}

func TestIrregularAndLate(t *testing.T) {
	s := Summarize(history("2026-01-01", "2026-01-23", "2026-02-28", "2026-03-24"), d("2026-05-15"))
	if !s.Irregular {
		t.Fatalf("cycles 22/36/24 should be irregular: %+v", s.Cycles)
	}
	if s.LateDays <= 0 {
		t.Fatalf("want late, got %d (next %s ±%d)", s.LateDays, ds(s.Predictions[0].Start), s.Predictions[0].RangeDays)
	}
	if r := s.Predictions[0].RangeDays; r < 1 || r > 7 {
		t.Fatalf("range %d outside 1..7", r)
	}
}

func TestBacktestSynthetic(t *testing.T) {
	// A year and a half of cycles of 29 ± 2 days: the method should be off by
	// about the noise, and a perfectly regular history by nothing.
	rng := rand.New(rand.NewSource(1))
	start := d("2025-01-01")
	var starts []string
	for i := 0; i < 18; i++ {
		starts = append(starts, ds(start))
		start = addDays(start, 27+rng.Intn(5))
	}
	mae, n := Backtest(history(starts...))
	if n != 15 || mae > 2.5 {
		t.Fatalf("mae %.2f over %d predictions", mae, n)
	}

	var regular []string
	for i := 0; i < 8; i++ {
		regular = append(regular, ds(addDays(d("2025-01-01"), 28*i)))
	}
	if mae, _ := Backtest(history(regular...)); mae != 0 {
		t.Fatalf("regular history mae %.2f", mae)
	}
}

func TestOngoingPeriodExpectsTheRest(t *testing.T) {
	// Three finished 5-day periods, then day 2 of the current one.
	logged := append(history("2026-01-01", "2026-01-29", "2026-02-26"), period("2026-03-26", 2)...)
	s := Summarize(logged, d("2026-03-27"))
	if s.Ongoing == nil || ds(s.Ongoing.Start) != "2026-03-28" || ds(s.Ongoing.End) != "2026-03-30" {
		t.Fatalf("ongoing %+v", s.Ongoing)
	}
	// Nothing logged for two days: it's over, nothing more is expected.
	if s := Summarize(logged, d("2026-03-29")); s.Ongoing != nil {
		t.Fatalf("stale period still ongoing: %+v", s.Ongoing)
	}
	// Already as long as usual: nothing more expected either.
	full := append(history("2026-01-01", "2026-01-29", "2026-02-26"), period("2026-03-26", 5)...)
	if s := Summarize(full, d("2026-03-30")); s.Ongoing != nil {
		t.Fatalf("full-length period still ongoing: %+v", s.Ongoing)
	}
}
