// Package cycle finds periods in logged flow days and predicts the next ones
// with the calendar method: the standard approach when all you have is
// period dates. It is pure (no database), so it is table-tested directly.
package cycle

import (
	"math"
	"sort"
	"time"
)

const (
	// Cycles outside these lengths are almost always a missed or doubled
	// log, so they are shown but left out of the statistics.
	MinCycle = 15
	MaxCycle = 60
	// Usable cycles needed before predicting at all.
	MinCycles = 2
	// Only the most recent windowMax usable cycles are used, weighted towards
	// the latest, so a drift over time shows up quickly.
	windowMax = 12
	// The ± range of the next start, in days.
	minRange = 1
	maxRange = 7
	// Spread (longest − shortest) above which cycles count as irregular,
	// after FIGO's 7–9 day definition.
	irregularSpread = 7
	// Predict this many cycles ahead.
	ahead = 3
	// Used to draw predicted periods before any has been logged in full.
	defaultPeriodLength = 5
	// The luteal phase is the more stable half of the cycle.
	lutealDays = 14
)

const DateLayout = "2006-01-02"

// Flow values, lightest first.
const (
	None     = "none"
	Spotting = "spotting"
	Light    = "light"
	Medium   = "medium"
	Heavy    = "heavy"
)

func ValidFlow(f string) bool {
	switch f {
	case None, Spotting, Light, Medium, Heavy:
		return true
	}
	return false
}

func bleeding(f string) bool { return f == Light || f == Medium || f == Heavy }

// Day is one logged day. Date is midnight UTC of the calendar date.
type Day struct {
	Date time.Time
	Flow string
}

type Period struct {
	Start  time.Time `json:"-"`
	End    time.Time `json:"-"` // last bleeding day, inclusive
	Length int       `json:"length"`
}

type Cycle struct {
	Start    time.Time `json:"-"`
	Length   int       `json:"length"`
	Excluded bool      `json:"excluded"` // outside MinCycle..MaxCycle
}

type Prediction struct {
	Start        time.Time
	End          time.Time // inclusive
	RangeDays    int
	Ovulation    time.Time
	FertileStart time.Time
	FertileEnd   time.Time // inclusive
}

type Summary struct {
	Periods      []Period
	Cycles       []Cycle
	Enough       bool
	NeedPeriods  int // more periods to log before predictions appear
	MeanCycle    float64
	SDCycle      float64
	PeriodLength int
	Irregular    bool
	Predictions  []Prediction
	LateDays     int
}

func days(a, b time.Time) int { return int(math.Round(b.Sub(a).Hours() / 24)) }

func addDays(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }

// Detect groups logged days into periods: runs of light-or-heavier days,
// allowing one unlogged (or "none") day inside a run. Spotting bridges days
// inside a period but never starts or ends one.
func Detect(logged []Day) []Period {
	ds := append([]Day(nil), logged...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Date.Before(ds[j].Date) })
	spotting := map[time.Time]bool{}
	var bleeds []time.Time
	for _, d := range ds {
		if d.Flow == Spotting {
			spotting[d.Date] = true
		} else if bleeding(d.Flow) {
			bleeds = append(bleeds, d.Date)
		}
	}

	var out []Period
	for i, b := range bleeds {
		if i > 0 {
			prev := bleeds[i-1]
			gap := 0
			for d := addDays(prev, 1); d.Before(b); d = addDays(d, 1) {
				if !spotting[d] {
					gap++
				}
			}
			if gap <= 1 {
				p := &out[len(out)-1]
				p.End = b
				p.Length = days(p.Start, b) + 1
				continue
			}
		}
		out = append(out, Period{Start: b, End: b, Length: 1})
	}
	return out
}

// Cycles is the length of each cycle between consecutive period starts.
func Cycles(periods []Period) []Cycle {
	var out []Cycle
	for i := 1; i < len(periods); i++ {
		n := days(periods[i-1].Start, periods[i].Start)
		out = append(out, Cycle{Start: periods[i-1].Start, Length: n, Excluded: n < MinCycle || n > MaxCycle})
	}
	return out
}

// Summarize detects periods and predicts the next ones as of today
// (midnight UTC of the local date).
func Summarize(logged []Day, today time.Time) Summary {
	return summarize(Detect(logged), today)
}

func summarize(periods []Period, today time.Time) Summary {
	s := Summary{Periods: periods, Cycles: Cycles(periods), PeriodLength: periodLength(periods, today)}

	var usable []int
	for _, c := range s.Cycles {
		if !c.Excluded {
			usable = append(usable, c.Length)
		}
	}
	if len(usable) < MinCycles {
		s.NeedPeriods = MinCycles - len(usable)
		return s
	}
	s.Enough = true

	window := usable
	if len(window) > windowMax {
		window = window[len(window)-windowMax:]
	}
	s.MeanCycle, s.SDCycle = weightedStats(window)
	lo, hi := window[0], window[0]
	for _, n := range window {
		lo, hi = min(lo, n), max(hi, n)
	}
	s.Irregular = hi-lo > irregularSpread

	last := periods[len(periods)-1].Start
	baseRange := clamp(int(math.Round(s.SDCycle)), minRange, maxRange)
	for k := 1; k <= ahead; k++ {
		start := addDays(last, int(math.Round(float64(k)*s.MeanCycle)))
		ov := addDays(start, -lutealDays)
		s.Predictions = append(s.Predictions, Prediction{
			Start:        start,
			End:          addDays(start, s.PeriodLength-1),
			RangeDays:    clamp(int(math.Round(float64(baseRange)*math.Sqrt(float64(k)))), minRange, 2*maxRange),
			Ovulation:    ov,
			FertileStart: addDays(ov, -5),
			FertileEnd:   addDays(ov, 1),
		})
	}
	if next := s.Predictions[0]; today.After(addDays(next.Start, next.RangeDays)) {
		s.LateDays = days(next.Start, today)
	}
	return s
}

// weightedStats is the mean and standard deviation of xs, weighting the
// i-th (oldest first) by i+1 so recent cycles count most.
func weightedStats(xs []int) (mean, sd float64) {
	var sum, wsum float64
	for i, x := range xs {
		w := float64(i + 1)
		sum += w * float64(x)
		wsum += w
	}
	mean = sum / wsum
	var v float64
	for i, x := range xs {
		d := float64(x) - mean
		v += float64(i+1) * d * d
	}
	return mean, math.Sqrt(v / wsum)
}

// periodLength is the median length of the last six finished periods; one
// that ended yesterday or later may still be going, so it's skipped.
func periodLength(periods []Period, today time.Time) int {
	var ls []int
	for _, p := range periods {
		if days(p.End, today) >= 2 {
			ls = append(ls, p.Length)
		}
	}
	if len(ls) == 0 {
		return defaultPeriodLength
	}
	if len(ls) > 6 {
		ls = ls[len(ls)-6:]
	}
	sort.Ints(ls)
	if len(ls)%2 == 1 {
		return ls[len(ls)/2]
	}
	return int(math.Round(float64(ls[len(ls)/2-1]+ls[len(ls)/2]) / 2))
}

func clamp(n, lo, hi int) int { return max(lo, min(hi, n)) }

// Backtest replays the history one period at a time: predict each start from
// the periods before it and compare with what was logged. It returns the mean
// absolute error in days over the predictions it could make. Starts after an
// excluded (implausible) cycle are skipped, since the log, not the method,
// is wrong there.
func Backtest(logged []Day) (mae float64, n int) {
	periods := Detect(logged)
	var total int
	for i := 1; i < len(periods); i++ {
		gap := days(periods[i-1].Start, periods[i].Start)
		if gap < MinCycle || gap > MaxCycle {
			continue
		}
		s := summarize(periods[:i], periods[i].Start)
		if !s.Enough {
			continue
		}
		e := days(s.Predictions[0].Start, periods[i].Start)
		if e < 0 {
			e = -e
		}
		total += e
		n++
	}
	if n == 0 {
		return 0, 0
	}
	return float64(total) / float64(n), n
}
