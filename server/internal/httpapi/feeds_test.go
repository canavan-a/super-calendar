package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/calendars"
)

// fakeFeeds stands in for the internet: each link answers with a status and
// body, and an ETag that a conditional request can match.
type fakeFeeds struct {
	mu    sync.Mutex
	links map[string]fakeLink
	reqs  []calendars.FetchRequest
}

type fakeLink struct {
	status int
	body   string
	etag   string
}

func (f *fakeFeeds) set(url string, status int, body, etag string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[url] = fakeLink{status, body, etag}
}

func (f *fakeFeeds) last() calendars.FetchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeFeeds) fetch(_ context.Context, req calendars.FetchRequest) (*calendars.FetchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	l, ok := f.links[req.URL]
	if !ok {
		l.status = 404
	}
	if l.status != 200 {
		return nil, &calendars.FetchError{Status: l.status, Msg: fmt.Sprintf("the link answered %d", l.status)}
	}
	if l.etag != "" && req.ETag == l.etag {
		return &calendars.FetchResponse{NotModified: true, ETag: l.etag}, nil
	}
	return &calendars.FetchResponse{Body: l.body, ETag: l.etag}, nil
}

// clock is a time the test moves along.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const secretLink = "https://calendar.example.com/calendar/ical/ana%40x.co/private-s3cr3t/basic.ics"

func feedICS(events ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nX-WR-CALNAME:Work\r\n" +
		strings.Join(events, "") + "END:VCALENDAR\r\n"
}

func vevent(uid, date, title string) string {
	d, _ := time.Parse("2006-01-02", date)
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART;VALUE=DATE:" + d.Format("20060102") +
		"\r\nDTEND;VALUE=DATE:" + d.AddDate(0, 0, 1).Format("20060102") + "\r\nSUMMARY:" + title + "\r\nEND:VEVENT\r\n"
}

type feedEnv struct {
	*env
	feeds *fakeFeeds
	clock *clock
}

func newFeedEnv(t *testing.T) *feedEnv {
	e := newEnv(t)
	f := &fakeFeeds{links: map[string]fakeLink{}}
	c := &clock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	e.cal.UseFetcher(f.fetch)
	e.cal.UseClock(c.now)
	return &feedEnv{env: e, feeds: f, clock: c}
}

func (e *feedEnv) subscribe(token, url string) calendars.Calendar {
	e.t.Helper()
	var c calResp
	e.mustCall(token, "POST", "/calendars", map[string]string{"kind": "subscribed", "name": "Work", "color": "#336699", "url": url, "tz": "Europe/London"}, 201, &c)
	return c.Calendar
}

func (e *feedEnv) titles(token, cal string) string {
	e.t.Helper()
	var occ occResp
	e.mustCall(token, "GET", "/calendars/"+cal+"/events?from=2026-01-01&to=2026-02-01", nil, 200, &occ)
	var out []string
	for _, o := range occ.Occurrences {
		out = append(out, o.Start+" "+o.Title)
	}
	return strings.Join(out, ", ")
}

func (e *feedEnv) feed(token, cal string) calendars.Feed {
	e.t.Helper()
	var f struct{ Feed calendars.Feed }
	e.mustCall(token, "GET", "/calendars/"+cal+"/feed", nil, 200, &f)
	return f.Feed
}

// sync moves the clock past the next sync and runs the job.
func (e *feedEnv) sync() {
	e.clock.add(16 * time.Minute)
	e.cal.SyncDueFeeds(context.Background())
}

func TestSubscribedCalendarMirrorsItsFeed(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup"), vevent("b", "2026-01-06", "Review")), `"v1"`)

	var p calendars.ImportPreview
	e.mustCall(ana, "POST", "/subscriptions/preview", map[string]string{"url": secretLink}, 200, &p)
	if p.Name != "Work" || p.Total != 2 || len(p.Sample) != 2 {
		t.Fatalf("preview %+v", p)
	}

	c := e.subscribe(ana, secretLink)
	if c.Kind != "subscribed" || c.Feed == nil || c.Feed.Status != "ok" || c.Feed.LastSyncedAt == nil {
		t.Fatalf("new calendar %+v", c)
	}
	// It opens with the feed's events already in it.
	if got := e.titles(ana, c.ID); got != "2026-01-05 Standup, 2026-01-06 Review" {
		t.Fatalf("after subscribing: %s", got)
	}

	// Not due yet: nothing is fetched.
	n := len(e.feeds.reqs)
	e.clock.add(5 * time.Minute)
	if e.cal.SyncDueFeeds(context.Background()); len(e.feeds.reqs) != n {
		t.Fatal("fetched before it was due")
	}

	// The feed answers "not modified" to the ETag it gave.
	e.sync()
	if last := e.feeds.last(); last.ETag != `"v1"` {
		t.Fatalf("conditional request sent ETag %q", last.ETag)
	}

	// Renamed, removed and added events all come through.
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Daily standup"), vevent("c", "2026-01-07", "Offsite")), `"v2"`)
	e.sync()
	if got := e.titles(ana, c.ID); got != "2026-01-05 Daily standup, 2026-01-07 Offsite" {
		t.Fatalf("after the feed changed: %s", got)
	}

	// A feed that's different text but the same events rewrites nothing.
	var before struct{ Event calendars.Event }
	var occ occResp
	e.mustCall(ana, "GET", "/calendars/"+c.ID+"/events?from=2026-01-01&to=2026-02-01", nil, 200, &occ)
	e.mustCall(ana, "GET", "/calendars/"+c.ID+"/events/"+occ.Occurrences[0].EventID, nil, 200, &before)
	e.feeds.set(secretLink, 200, strings.Replace(feedICS(vevent("a", "2026-01-05", "Daily standup"), vevent("c", "2026-01-07", "Offsite")),
		"PRODID:-//Test//EN", "PRODID:-//Test//EN v2", 1), `"v3"`)
	e.sync()
	var after struct{ Event calendars.Event }
	e.mustCall(ana, "GET", "/calendars/"+c.ID+"/events/"+occ.Occurrences[0].EventID, nil, 200, &after)
	if after.Event.UpdatedAt != before.Event.UpdatedAt {
		t.Fatalf("an unchanged event was rewritten: %s → %s", before.Event.UpdatedAt, after.Event.UpdatedAt)
	}

	// It shows in the Overview as its own kind.
	var ov struct{ Items []calendars.Item }
	e.mustCall(ana, "GET", "/overview?from=2026-01-01&to=2026-02-01", nil, 200, &ov)
	if len(ov.Items) != 2 || ov.Items[0].Kind != "subscribed" || ov.Items[0].Layer != "event" {
		t.Fatalf("overview %+v", ov.Items)
	}
}

func TestSubscribedCalendarIsReadOnly(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup")), "")
	c := e.subscribe(ana, secretLink)
	var occ occResp
	e.mustCall(ana, "GET", "/calendars/"+c.ID+"/events?from=2026-01-01&to=2026-02-01", nil, 200, &occ)
	id := occ.Occurrences[0].EventID
	e.mustCall(ana, "GET", "/calendars/"+c.ID+"/events/"+id, nil, 200, nil)

	event := map[string]any{"title": "Mine now", "allDay": true, "start": "2026-01-08"}
	for _, p := range []string{
		"POST /events", "PATCH /events/" + id, "PATCH /events/" + id + "?occurrence=2026-01-05",
		"DELETE /events/" + id, "DELETE /events/" + id + "?occurrence=2026-01-05",
		"POST /import/preview", "POST /import",
	} {
		method, path, _ := strings.Cut(p, " ")
		if got := e.call(ana, method, "/calendars/"+c.ID+path, event, nil); got != 404 {
			t.Errorf("%s on a subscribed calendar: %d, want 404", p, got)
		}
	}
	if got := e.titles(ana, c.ID); got != "2026-01-05 Standup" {
		t.Fatalf("events changed: %s", got)
	}
	// The calendar itself is still the members' to change.
	e.mustCall(ana, "PATCH", "/calendars/"+c.ID, map[string]string{"name": "Office"}, 200, nil)
	e.mustCall(ana, "PATCH", "/calendars/"+c.ID+"/me", map[string]string{"color": "#aa0000"}, 200, nil)

	// One can't be made without a link, and feed routes are for subscribed
	// calendars only.
	e.mustCall(ana, "POST", "/calendars", map[string]string{"kind": "subscribed", "name": "X", "color": "#336699"}, 400, nil)
	general := e.create(ana, "general", "Family")
	e.mustCall(ana, "GET", "/calendars/"+general+"/feed", nil, 404, nil)
}

func TestFeedTroubleKeepsEvents(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	bo := e.signUp("bo@x.co", "Bo")
	good := feedICS(vevent("a", "2026-01-05", "Standup"))
	e.feeds.set(secretLink, 200, good, "")
	// A bad link makes nothing.
	e.mustCall(ana, "POST", "/calendars", map[string]string{"kind": "subscribed", "name": "W", "color": "#336699", "url": "https://calendar.example.com/nope"}, 400, nil)
	e.mustCall(ana, "POST", "/calendars", map[string]string{"kind": "subscribed", "name": "W", "color": "#336699", "url": "ftp://x"}, 400, nil)
	c := e.subscribe(ana, secretLink)

	steps := []struct {
		status    int
		body      string
		want      string
		nextAfter time.Duration
	}{
		{500, "", "failing", 15 * time.Minute},
		{200, "<html>Sign in</html>", "failing", 30 * time.Minute},
		{503, "", "failing", time.Hour},
		{404, "", "broken", 24 * time.Hour},
	}
	for _, s := range steps {
		e.feeds.set(secretLink, s.status, s.body, "")
		e.clock.add(7 * time.Hour) // past any backoff
		e.cal.SyncDueFeeds(context.Background())
		f := e.feed(ana, c.ID)
		if f.Status != s.want || f.LastError == "" || strings.Contains(f.LastError, "s3cr3t") {
			t.Fatalf("after %d: %+v", s.status, f)
		}
		next, _ := time.Parse(time.RFC3339, f.NextSyncAt)
		if got := next.Sub(e.clock.now()); got != s.nextAfter {
			t.Fatalf("after %d: next sync in %v, want %v", s.status, got, s.nextAfter)
		}
		if got := e.titles(ana, c.ID); got != "2026-01-05 Standup" {
			t.Fatalf("after %d the events are: %q", s.status, got)
		}
	}
	var list struct{ Calendars []calendars.Calendar }
	e.mustCall(ana, "GET", "/calendars", nil, 200, &list)
	if list.Calendars[0].Feed.Status != "broken" {
		t.Fatalf("list shows %+v", list.Calendars[0].Feed)
	}

	// Ana has left; Bo, still in it, pastes the new link. A bad one first
	// changes nothing.
	var inv struct{ Invite calendars.Invite }
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/invites", map[string]string{"email": "bo@x.co"}, 201, &inv)
	e.mustCall(bo, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 200, nil)
	e.mustCall(ana, "DELETE", "/calendars/"+c.ID+"/me", nil, 204, nil)
	e.mustCall(bo, "PUT", "/calendars/"+c.ID+"/feed", map[string]string{"url": secretLink}, 400, nil)
	newLink := strings.Replace(secretLink, "s3cr3t", "n3w", 1)
	e.feeds.set(newLink, 200, feedICS(vevent("a", "2026-01-05", "Standup"), vevent("b", "2026-01-09", "Retro")), "")
	var put struct{ Feed calendars.Feed }
	e.mustCall(bo, "PUT", "/calendars/"+c.ID+"/feed", map[string]string{"url": newLink}, 200, &put)
	if put.Feed.Status != "ok" || !put.Feed.Mine || put.Feed.URL != newLink || put.Feed.LastError != "" {
		t.Fatalf("after replacing: %+v", put.Feed)
	}
	if got := e.titles(bo, c.ID); got != "2026-01-05 Standup, 2026-01-09 Retro" {
		t.Fatalf("after replacing: %s", got)
	}
}

func TestOneEmptyFeedIsHeldBack(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup")), `"v1"`)
	c := e.subscribe(ana, secretLink)

	e.feeds.set(secretLink, 200, feedICS(), `"v2"`)
	e.sync()
	if got := e.titles(ana, c.ID); got != "2026-01-05 Standup" {
		t.Fatalf("one empty feed emptied it: %q", got)
	}
	// The next sync downloads it again (not "not modified" against the
	// empty one) and, still empty, empties the calendar.
	e.sync()
	if last := e.feeds.last(); last.ETag != `"v1"` {
		t.Fatalf("second fetch sent ETag %q, want the last applied one", last.ETag)
	}
	if got := e.titles(ana, c.ID); got != "" {
		t.Fatalf("two empty feeds in a row left: %q", got)
	}
	// An empty feed after an empty calendar is just empty.
	e.feeds.set(secretLink, 200, feedICS(vevent("b", "2026-01-06", "Back")), `"v3"`)
	e.sync()
	if got := e.titles(ana, c.ID); got != "2026-01-06 Back" {
		t.Fatalf("after it came back: %q", got)
	}
}

func TestFeedLinkIsSecret(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	bo := e.signUp("bo@x.co", "Bo")
	cy := e.signUp("cy@x.co", "Cy")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup")), "")
	c := e.subscribe(ana, secretLink)
	var inv struct{ Invite calendars.Invite }
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/invites", map[string]string{"email": "bo@x.co"}, 201, &inv)
	e.mustCall(bo, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 200, nil)

	if f := e.feed(ana, c.ID); !f.Mine || f.URL != secretLink || f.Host != "calendar.example.com" || f.EventCount != 1 {
		t.Fatalf("ana's view %+v", f)
	}
	if f := e.feed(bo, c.ID); f.Mine || f.URL != "" || f.Host != "calendar.example.com" || f.AddedByName != "Ana" {
		t.Fatalf("bo's view %+v", f)
	}
	// Nothing else a member is sent carries it.
	for _, path := range []string{"/calendars", "/calendars/" + c.ID, "/calendars/" + c.ID + "/members", "/overview?from=2026-01-01&to=2026-02-01"} {
		var raw json.RawMessage
		e.mustCall(bo, "GET", path, nil, 200, &raw)
		if strings.Contains(string(raw), "s3cr3t") {
			t.Fatalf("%s carries the link: %s", path, raw)
		}
	}
	// Non-members can't tell it exists.
	for _, p := range []string{"GET /feed", "PUT /feed", "POST /feed/refresh", "GET /events?from=2026-01-01&to=2026-02-01"} {
		method, path, _ := strings.Cut(p, " ")
		if got := e.call(cy, method, "/calendars/"+c.ID+path, map[string]string{"url": secretLink}, nil); got != 404 {
			t.Errorf("%s by a non-member: %d", p, got)
		}
	}
}

func TestRefreshNowIsLimited(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup")), "")
	c := e.subscribe(ana, secretLink)
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup moved"), vevent("b", "2026-01-06", "New")), "")

	var f struct{ Feed calendars.Feed }
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/feed/refresh", nil, 200, &f)
	if f.Feed.EventCount != 2 {
		t.Fatalf("after refreshing: %+v", f.Feed)
	}
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/feed/refresh", nil, 429, nil)
	e.clock.add(61 * time.Second)
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/feed/refresh", nil, 200, nil)
	// A link in trouble isn't an error to refresh: it's in the status.
	e.feeds.set(secretLink, 500, "", "")
	e.clock.add(61 * time.Second)
	e.mustCall(ana, "POST", "/calendars/"+c.ID+"/feed/refresh", nil, 200, &f)
	if f.Feed.Status != "failing" {
		t.Fatalf("refresh of a failing link: %+v", f.Feed)
	}
}

func TestFeedSyncTellsMembers(t *testing.T) {
	e := newFeedEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Standup")), "")
	c := e.subscribe(ana, secretLink)
	ch := e.listen(ana)

	// Nothing changed: nobody is bothered.
	e.sync()
	quiet(t, ch)

	e.feeds.set(secretLink, 200, feedICS(vevent("a", "2026-01-05", "Moved")), "")
	e.sync()
	if m := next(t, ch); m.Type != "changed" || m.CalendarID != c.ID || m.Area != "events" || m.By != "" {
		t.Fatalf("after a sync that changed events: %+v", m)
	}
	// A change of status reloads the calendar (the list shows it).
	e.feeds.set(secretLink, 404, "", "")
	e.sync()
	if m := next(t, ch); m.Area != "calendar" {
		t.Fatalf("after the link broke: %+v", m)
	}
}
