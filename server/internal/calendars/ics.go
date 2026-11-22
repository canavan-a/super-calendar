package calendars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/google/uuid"
)

const (
	maxICSBytes  = 5 << 20
	maxICSEvents = 5000
	fetchTimeout = 15 * time.Second
)

// ImportRequest is an .ics file's text, or a link to one for the server to
// fetch. TZ is the importer's zone, used for times the file doesn't zone.
type ImportRequest struct {
	ICS string `json:"ics"`
	URL string `json:"url"`
	TZ  string `json:"tz"`
}

type ImportPreview struct {
	Name    string   `json:"name"` // X-WR-CALNAME, if the file has one
	Total   int      `json:"total"`
	New     int      `json:"new"`
	Updates int      `json:"updates"`
	Errors  []string `json:"errors"`
	Sample  []*Event `json:"sample"` // the first few, for the preview list
}

type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

// Fetcher downloads a calendar link. Tests swap it out.
type Fetcher func(ctx context.Context, rawURL string) (string, error)

func (s *Service) parseImport(ctx context.Context, calendarID string, req ImportRequest) (string, []*Event, []string, error) {
	text := req.ICS
	if text == "" && req.URL != "" {
		var err error
		if text, err = s.fetch(ctx, req.URL); err != nil {
			return "", nil, nil, err
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", nil, nil, invalid("there's no calendar to import")
	}
	loc := time.UTC
	if l, err := time.LoadLocation(req.TZ); err == nil && req.TZ != "" {
		loc = l
	}
	return ParseICS(text, calendarID, loc)
}

// ParseICS turns iCalendar text into events (not yet saved). Events that
// can't be read are reported in errs and left out.
func ParseICS(text, calendarID string, defaultLoc *time.Location) (name string, events []*Event, errs []string, err error) {
	cal, err := ics.ParseCalendar(strings.NewReader(text))
	if err != nil {
		return "", nil, nil, invalid("that isn't a calendar file (%v)", err)
	}
	for _, p := range cal.CalendarProperties {
		if p.IANAToken == "X-WR-CALNAME" {
			name = unescapeText(p.Value)
		}
	}
	errs = []string{}
	for i, ve := range cal.Events() {
		if len(events) >= maxICSEvents {
			errs = append(errs, fmt.Sprintf("only the first %d events were read", maxICSEvents))
			break
		}
		e, err := fromVEvent(ve, calendarID, defaultLoc)
		if err != nil {
			title := propValue(ve, ics.ComponentPropertySummary)
			if title == "" {
				title = fmt.Sprintf("event %d", i+1)
			}
			errs = append(errs, fmt.Sprintf("%s: %v", title, err))
			continue
		}
		events = append(events, e)
	}
	return name, events, errs, nil
}

func propValue(ve *ics.VEvent, p ics.ComponentProperty) string {
	if prop := ve.GetProperty(p); prop != nil {
		return unescapeText(prop.Value)
	}
	return ""
}

func unescapeText(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(s))
}

func fromVEvent(ve *ics.VEvent, calendarID string, defaultLoc *time.Location) (*Event, error) {
	if strings.EqualFold(propValue(ve, ics.ComponentPropertyStatus), "CANCELLED") && ve.GetProperty(ics.ComponentPropertyRecurrenceId) == nil {
		return nil, errors.New("cancelled")
	}
	startProp := ve.GetProperty(ics.ComponentPropertyDtStart)
	if startProp == nil {
		return nil, errors.New("no start time")
	}
	start, allDay, tzid, err := parseICSTime(&startProp.BaseProperty, defaultLoc)
	if err != nil {
		return nil, err
	}
	e := &Event{
		ID: uuid.NewString(), CalendarID: calendarID, Source: "ics", AllDay: allDay, TZID: tzid, ExDates: []string{},
		Title:       propValue(ve, ics.ComponentPropertySummary),
		Description: propValue(ve, ics.ComponentPropertyDescription),
		Location:    propValue(ve, ics.ComponentPropertyLocation),
		URL:         propValue(ve, ics.ComponentPropertyUrl),
	}
	var end time.Time
	if p := ve.GetProperty(ics.ComponentPropertyDtEnd); p != nil {
		if end, _, _, err = parseICSTime(&p.BaseProperty, defaultLoc); err != nil {
			return nil, err
		}
	} else if p := ve.GetProperty(ics.ComponentPropertyDuration); p != nil {
		d, err := parseICSDuration(p.Value)
		if err != nil {
			return nil, err
		}
		end = start.Add(d)
	} else if allDay {
		end = start.AddDate(0, 0, 1)
	} else {
		end = start
	}
	e.Start, e.End = formatEventTime(start, allDay), formatEventTime(end, allDay)

	uid := propValue(ve, ics.ComponentPropertyUniqueId)
	if uid == "" {
		// No UID: derive one from the content so re-importing still dedupes.
		h := sha256.Sum256([]byte(e.Title + "|" + e.Start + "|" + e.End))
		uid = "noid-" + hex.EncodeToString(h[:12])
	}
	e.UID = uid
	if p := ve.GetProperty(ics.ComponentPropertyRecurrenceId); p != nil {
		rid, _, _, err := parseICSTime(&p.BaseProperty, defaultLoc)
		if err != nil {
			return nil, err
		}
		e.RecurrenceID = formatEventTime(rid, allDay)
		e.Cancelled = strings.EqualFold(propValue(ve, ics.ComponentPropertyStatus), "CANCELLED")
	} else if p := ve.GetProperty(ics.ComponentPropertyRrule); p != nil {
		e.RRule = p.Value
	}
	for _, p := range ve.GetProperties(ics.ComponentPropertyExdate) {
		for _, v := range strings.Split(p.Value, ",") {
			bp := ics.BaseProperty{Value: v, ICalParameters: p.ICalParameters}
			t, _, _, err := parseICSTime(&bp, defaultLoc)
			if err == nil {
				e.ExDates = append(e.ExDates, formatEventTime(t, allDay))
			}
		}
	}
	if err := normalizeEvent(e); err != nil {
		return nil, err
	}
	return e, nil
}

func formatEventTime(t time.Time, allDay bool) string {
	if allDay {
		return t.Format(dateLayout)
	}
	return t.UTC().Format(time.RFC3339)
}

// Outlook and Exchange write Windows zone names; these are the common ones.
var windowsZones = map[string]string{
	"Eastern Standard Time": "America/New_York", "Central Standard Time": "America/Chicago",
	"Mountain Standard Time": "America/Denver", "Pacific Standard Time": "America/Los_Angeles",
	"US Mountain Standard Time": "America/Phoenix", "Alaskan Standard Time": "America/Anchorage",
	"Hawaiian Standard Time": "Pacific/Honolulu", "Atlantic Standard Time": "America/Halifax",
	"GMT Standard Time": "Europe/London", "Greenwich Standard Time": "Atlantic/Reykjavik",
	"W. Europe Standard Time": "Europe/Berlin", "Romance Standard Time": "Europe/Paris",
	"Central Europe Standard Time": "Europe/Budapest", "Central European Standard Time": "Europe/Warsaw",
	"E. Europe Standard Time": "Europe/Chisinau", "FLE Standard Time": "Europe/Kiev",
	"GTB Standard Time": "Europe/Bucharest", "Russian Standard Time": "Europe/Moscow",
	"India Standard Time": "Asia/Kolkata", "China Standard Time": "Asia/Shanghai",
	"Tokyo Standard Time": "Asia/Tokyo", "Korea Standard Time": "Asia/Seoul",
	"Singapore Standard Time": "Asia/Singapore", "AUS Eastern Standard Time": "Australia/Sydney",
	"E. Australia Standard Time": "Australia/Brisbane", "Cen. Australia Standard Time": "Australia/Adelaide",
	"W. Australia Standard Time": "Australia/Perth", "New Zealand Standard Time": "Pacific/Auckland",
	"UTC": "UTC", "Coordinated Universal Time": "UTC",
}

func loadZone(tzid string) (*time.Location, bool) {
	tzid = strings.Trim(tzid, `"`)
	if tzid == "" {
		return nil, false
	}
	if l, err := time.LoadLocation(tzid); err == nil {
		return l, true
	}
	if name, ok := windowsZones[tzid]; ok {
		if l, err := time.LoadLocation(name); err == nil {
			return l, true
		}
	}
	// "/mozilla.org/20050126_1/America/New_York" and the like.
	if i := strings.Index(tzid, "/"); i >= 0 {
		parts := strings.Split(tzid, "/")
		for j := range parts {
			if l, err := time.LoadLocation(strings.Join(parts[j:], "/")); err == nil {
				return l, true
			}
		}
	}
	return nil, false
}

// parseICSTime reads a DATE or DATE-TIME property. It returns the time, if
// it's a whole day, and the zone name it's in.
func parseICSTime(p *ics.BaseProperty, defaultLoc *time.Location) (time.Time, bool, string, error) {
	v := strings.TrimSpace(p.Value)
	isDate := len(v) == 8
	if vals := p.ICalParameters["VALUE"]; len(vals) > 0 && strings.EqualFold(vals[0], "DATE") {
		isDate = true
	}
	if isDate {
		if len(v) > 8 {
			v = v[:8]
		}
		t, err := time.Parse("20060102", v)
		if err != nil {
			return time.Time{}, false, "", fmt.Errorf("bad date %q", v)
		}
		return t, true, "UTC", nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		if err != nil {
			return time.Time{}, false, "", fmt.Errorf("bad time %q", v)
		}
		// Keep the importer's zone so a UTC-written weekly event still
		// repeats at the same local time.
		return t, false, defaultLoc.String(), nil
	}
	loc := defaultLoc
	if tz := p.ICalParameters["TZID"]; len(tz) > 0 {
		if l, ok := loadZone(tz[0]); ok {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	if err != nil {
		return time.Time{}, false, "", fmt.Errorf("bad time %q", v)
	}
	return t, false, loc.String(), nil
}

// parseICSDuration reads an RFC 5545 duration such as P1D, PT1H30M or P2W.
func parseICSDuration(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0, fmt.Errorf("bad duration %q", v)
	}
	var d time.Duration
	n := 0
	inTime := false
	for _, r := range v[1:] {
		switch {
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
			continue
		case r == 'T':
			inTime = true
			continue
		case r == 'W':
			d += time.Duration(n) * 7 * 24 * time.Hour
		case r == 'D':
			d += time.Duration(n) * 24 * time.Hour
		case r == 'H' && inTime:
			d += time.Duration(n) * time.Hour
		case r == 'M' && inTime:
			d += time.Duration(n) * time.Minute
		case r == 'S' && inTime:
			d += time.Duration(n) * time.Second
		default:
			return 0, fmt.Errorf("bad duration %q", v)
		}
		n = 0
	}
	if neg {
		d = -d
	}
	return d, nil
}

// existing is the calendar's events by UID and RECURRENCE-ID, what an import
// dedupes on.
func (s *Service) existing(ctx context.Context, calendarID string) (map[string]*Event, error) {
	events, err := s.queryEvents(ctx, `WHERE e.calendar_id = ?`, calendarID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*Event, len(events))
	for _, e := range events {
		out[e.UID+"|"+e.RecurrenceID] = e
	}
	return out, nil
}

func (s *Service) PreviewImport(ctx context.Context, calendarID string, req ImportRequest) (*ImportPreview, error) {
	name, events, errs, err := s.parseImport(ctx, calendarID, req)
	if err != nil {
		return nil, err
	}
	existing, err := s.existing(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	p := &ImportPreview{Name: name, Total: len(events), Errors: errs, Sample: []*Event{}}
	for _, e := range events {
		if _, ok := existing[e.UID+"|"+e.RecurrenceID]; ok {
			p.Updates++
		} else {
			p.New++
		}
		if len(p.Sample) < 50 {
			p.Sample = append(p.Sample, e)
		}
	}
	return p, nil
}

// Import saves the events, updating ones already imported with the same UID
// (and RECURRENCE-ID) instead of duplicating them.
func (s *Service) Import(ctx context.Context, calendarID, userID string, req ImportRequest) (*ImportResult, error) {
	_, events, errs, err := s.parseImport(ctx, calendarID, req)
	if err != nil {
		return nil, err
	}
	existing, err := s.existing(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res := &ImportResult{Errors: errs, Skipped: len(errs)}
	seen := map[string]bool{}
	for _, e := range events {
		key := e.UID + "|" + e.RecurrenceID
		if seen[key] {
			res.Skipped++
			continue
		}
		seen[key] = true
		if old, ok := existing[key]; ok {
			// Keep what was deleted in the app: occurrences removed from a
			// series, and cancelled overrides, stay gone.
			e.ID = old.ID
			e.ExDates = union(old.ExDates, e.ExDates)
			e.Cancelled = e.Cancelled || old.Cancelled
			e.Color = old.Color // a colour picked in the app
			if err := s.saveEvent(ctx, tx, e, userID); err != nil {
				return nil, err
			}
			res.Updated++
			continue
		}
		if err := s.insertEvent(ctx, tx, e, userID); err != nil {
			return nil, err
		}
		res.Imported++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// --- Fetching links ---

func (s *Service) fetch(ctx context.Context, rawURL string) (string, error) {
	if s.fetcher != nil {
		return s.fetcher(ctx, rawURL)
	}
	return fetchICS(ctx, rawURL)
}

var errPrivateAddress = errors.New("that address isn't on the public internet")

// publicOnly refuses connections to loopback, private, link-local and other
// non-public addresses, after DNS resolution, so a link can't make the server
// reach things on its own network.
func publicOnly(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return errPrivateAddress
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 { // 100.64/10 carrier NAT
		return errPrivateAddress
	}
	return nil
}

var fetchClient = &http.Client{
	Timeout: fetchTimeout,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, Control: publicOnly}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		Proxy:               nil,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirected somewhere that isn't a web link")
		}
		return nil
	},
}

func fetchICS(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", invalid("that isn't a link")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "webcal", "webcals", "https":
		u.Scheme = "https"
	case "http":
	default:
		return "", invalid("only web and webcal links can be imported")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", invalid("that isn't a link")
	}
	req.Header.Set("Accept", "text/calendar, */*;q=0.5")
	req.Header.Set("User-Agent", "SuperCalendar/1")
	resp, err := fetchClient.Do(req)
	if err != nil && scheme == "webcal" && !errors.Is(err, errPrivateAddress) {
		// Some webcal feeds are only served over plain http.
		u.Scheme = "http"
		req.URL = u
		resp, err = fetchClient.Do(req)
	}
	if err != nil {
		if errors.Is(err, errPrivateAddress) {
			return "", invalid("%v", errPrivateAddress)
		}
		return "", invalid("couldn't download that link")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", invalid("the link answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxICSBytes+1))
	if err != nil {
		return "", invalid("couldn't download that link")
	}
	if len(body) > maxICSBytes {
		return "", invalid("that calendar is over 5 MB")
	}
	return string(body), nil
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range [][]string{a, b} {
		for _, v := range list {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}
