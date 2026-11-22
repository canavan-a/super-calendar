package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/auth"
	"github.com/canavan-a/super-calendar/server/internal/calendars"
	"github.com/canavan-a/super-calendar/server/internal/config"
	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/canavan-a/super-calendar/server/internal/testdb"
)

type fakeMailer struct {
	codes   map[string]string
	invites []string
}

func (m *fakeMailer) SendLoginCode(to, code string) error { m.codes[to] = code; return nil }
func (m *fakeMailer) SendInvite(to, _, _, _ string) error {
	m.invites = append(m.invites, to)
	return nil
}

type env struct {
	t      *testing.T
	srv    *httptest.Server
	mailer *fakeMailer
	auth   *auth.Service
	store  *auth.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	conn := testdb.Open(t)
	mailer := &fakeMailer{codes: map[string]string{}}
	store := auth.NewStore(conn)
	as := auth.NewService(store, mailer)
	cs := calendars.NewService(conn, mailer)
	hub := live.NewHub()
	cs.UseLive(hub)
	srv := httptest.NewServer(NewRouter(Deps{
		Config: &config.Config{}, AuthService: as, AuthHandlers: auth.NewHandlers(as),
		Calendars: calendars.NewHandlers(cs), Live: hub,
	}))
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, mailer: mailer, auth: as, store: store}
}

// signUp makes an account and returns its token.
func (e *env) signUp(email, name string) string {
	e.t.Helper()
	ctx := context.Background()
	if err := e.auth.RequestCode(ctx, email, ""); err != nil {
		e.t.Fatalf("code for %s: %v", email, err)
	}
	_, token, err := e.auth.Verify(ctx, email, e.mailer.codes[email], name)
	if err != nil {
		e.t.Fatalf("verify %s: %v", email, err)
	}
	return token
}

// call makes a request and decodes the JSON answer into out (if non-nil),
// returning the status.
func (e *env) call(token, method, path string, body any, out any) int {
	e.t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *env) mustCall(token, method, path string, body any, want int, out any) {
	e.t.Helper()
	if got := e.call(token, method, path, body, out); got != want {
		e.t.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
}

type calResp struct {
	Calendar calendars.Calendar `json:"calendar"`
}

func (e *env) create(token, kind, name string) string {
	e.t.Helper()
	var c calResp
	e.mustCall(token, "POST", "/calendars", map[string]string{"kind": kind, "name": name, "color": "#336699"}, 201, &c)
	return c.Calendar.ID
}

func TestNonMembersGet404Everywhere(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	bo := e.signUp("bo@x.co", "Bo")
	ids := map[string]string{
		"general":  e.create(ana, "general", "Family"),
		"training": e.create(ana, "training", "Plan"),
		"cycle":    e.create(ana, "cycle", "Ana"),
	}
	paths := map[string][]string{
		"general":  {"GET /", "PATCH /", "PATCH /me", "DELETE /me", "GET /members", "GET /invites", "POST /invites", "GET /events?from=2026-01-01&to=2026-02-01", "POST /events", "POST /import/preview", "POST /import"},
		"training": {"GET /activity-types", "POST /activity-types", "GET /sessions?from=2026-01-01&to=2026-02-01", "GET /sessions/x", "POST /sessions", "POST /sessions/copy", "POST /sessions/restore"},
		"cycle":    {"GET /cycle/days", "PUT /cycle/days/2026-01-01", "GET /cycle/summary"},
	}
	for kind, ps := range paths {
		for _, p := range ps {
			method, path, _ := strings.Cut(p, " ")
			if got := e.call(bo, method, "/calendars/"+ids[kind]+strings.TrimSuffix(path, "/"), map[string]any{}, nil); got != 404 {
				t.Errorf("%s %s on someone else's %s calendar: %d, want 404", method, path, kind, got)
			}
		}
	}
	// A kind's routes don't exist on other kinds.
	if got := e.call(ana, "GET", "/calendars/"+ids["cycle"]+"/sessions?from=2026-01-01&to=2026-02-01", nil, nil); got != 404 {
		t.Errorf("training route on a cycle calendar: %d", got)
	}
	var list struct{ Calendars []calendars.Calendar }
	e.mustCall(bo, "GET", "/calendars", nil, 200, &list)
	if len(list.Calendars) != 0 {
		t.Fatalf("bo sees %d calendars", len(list.Calendars))
	}
	// The overview doesn't leak either.
	var ov struct{ Items []calendars.Item }
	e.mustCall(bo, "GET", "/overview?from=2026-01-01&to=2026-02-01", nil, 200, &ov)
	if len(ov.Items) != 0 {
		t.Fatalf("bo's overview has %d items", len(ov.Items))
	}
}

func TestInviteBeforeSignUpAcceptAndLeave(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Family")

	// Fill the server, then invite someone without an account.
	if err := e.store.SetUserCap(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var inv struct{ Invite calendars.Invite }
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": " Bo@X.co "}, 201, &inv)
	// Inviting again returns the same pending invite without another email.
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": "bo@x.co"}, 201, nil)
	if len(e.mailer.invites) != 1 || e.mailer.invites[0] != "bo@x.co" {
		t.Fatalf("invite emails %v", e.mailer.invites)
	}
	// The invite lets them past the full cap.
	bo := e.signUp("bo@x.co", "Bo")

	var inbox struct{ Invites []calendars.Invite }
	e.mustCall(bo, "GET", "/invites", nil, 200, &inbox)
	if len(inbox.Invites) != 1 || inbox.Invites[0].CalendarName != "Family" || inbox.Invites[0].InviterName != "Ana" {
		t.Fatalf("inbox %+v", inbox.Invites)
	}
	// Someone else can't answer it.
	e.mustCall(ana, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 404, nil)
	e.mustCall(bo, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 200, nil)
	e.mustCall(bo, "GET", "/calendars/"+cal, nil, 200, nil)
	e.mustCall(bo, "GET", "/invites", nil, 200, &inbox)
	if len(inbox.Invites) != 0 {
		t.Fatalf("inbox after accepting: %+v", inbox.Invites)
	}
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": "bo@x.co"}, 409, nil)

	var members struct{ Members []calendars.Member }
	e.mustCall(bo, "GET", "/calendars/"+cal+"/members", nil, 200, &members)
	if len(members.Members) != 2 {
		t.Fatalf("members %+v", members.Members)
	}

	// Ana leaves: Bo keeps the calendar. Then Bo leaves: it's gone.
	e.mustCall(ana, "DELETE", "/calendars/"+cal+"/me", nil, 204, nil)
	e.mustCall(ana, "GET", "/calendars/"+cal, nil, 404, nil)
	e.mustCall(bo, "POST", "/calendars/"+cal+"/events", map[string]any{"title": "x", "allDay": true, "start": "2026-01-01"}, 201, nil)
	e.mustCall(bo, "DELETE", "/calendars/"+cal+"/me", nil, 204, nil)
	// Bo was the last member, so the calendar went with them.
	e.mustCall(bo, "GET", "/calendars/"+cal, nil, 404, nil)
}

func TestAccountDeletionLeavesEverything(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	bo := e.signUp("bo@x.co", "Bo")
	solo := e.create(ana, "cycle", "Mine")
	shared := e.create(ana, "general", "Ours")
	var inv struct{ Invite calendars.Invite }
	e.mustCall(ana, "POST", "/calendars/"+shared+"/invites", map[string]string{"email": "bo@x.co"}, 201, &inv)
	e.mustCall(bo, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 200, nil)
	e.mustCall(ana, "PUT", "/calendars/"+solo+"/cycle/days/2026-01-01", map[string]string{"flow": "heavy"}, 204, nil)

	e.mustCall(ana, "DELETE", "/me", nil, 204, nil)

	var members struct{ Members []calendars.Member }
	e.mustCall(bo, "GET", "/calendars/"+shared+"/members", nil, 200, &members)
	if len(members.Members) != 1 {
		t.Fatalf("shared calendar members %+v", members.Members)
	}
	// Ana's own calendar went with her: signing up again doesn't bring it back.
	ana2 := e.signUp("ana@x.co", "Ana")
	e.mustCall(ana2, "GET", "/calendars/"+solo, nil, 404, nil)
}

type occResp struct {
	Occurrences []calendars.Occurrence `json:"occurrences"`
}

func TestRecurringEventsAcrossDST(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Work")
	var ev struct{ Event calendars.Event }
	// Weekly at 09:00 New York from Monday 2026-03-02; DST starts 2026-03-08.
	e.mustCall(ana, "POST", "/calendars/"+cal+"/events", map[string]any{
		"title": "Standup", "start": "2026-03-02T09:00:00-05:00", "end": "2026-03-02T09:15:00-05:00",
		"tzid": "America/New_York", "rrule": "FREQ=WEEKLY;COUNT=4",
	}, 201, &ev)

	var occ occResp
	e.mustCall(ana, "GET", "/calendars/"+cal+"/events?from=2026-03-01&to=2026-04-01&tz=America/New_York", nil, 200, &occ)
	want := []string{"2026-03-02T14:00:00Z", "2026-03-09T13:00:00Z", "2026-03-16T13:00:00Z", "2026-03-23T13:00:00Z"}
	if len(occ.Occurrences) != 4 {
		t.Fatalf("occurrences %+v", occ.Occurrences)
	}
	for i, o := range occ.Occurrences {
		if o.Start != want[i] {
			t.Errorf("occurrence %d at %s, want %s", i, o.Start, want[i])
		}
	}

	// Move just the second one an hour later, and delete the third.
	second := occ.Occurrences[1].Occurrence
	e.mustCall(ana, "PATCH", "/calendars/"+cal+"/events/"+ev.Event.ID+"?occurrence="+second, map[string]any{
		"start": "2026-03-09T10:00:00-04:00", "end": "2026-03-09T10:15:00-04:00",
	}, 200, nil)
	e.mustCall(ana, "DELETE", "/calendars/"+cal+"/events/"+ev.Event.ID+"?occurrence="+occ.Occurrences[2].Occurrence, nil, 204, nil)
	e.mustCall(ana, "GET", "/calendars/"+cal+"/events?from=2026-03-01&to=2026-04-01&tz=America/New_York", nil, 200, &occ)
	got := []string{}
	for _, o := range occ.Occurrences {
		got = append(got, o.Start)
	}
	if fmt.Sprint(got) != "[2026-03-02T14:00:00Z 2026-03-09T14:00:00Z 2026-03-23T13:00:00Z]" {
		t.Fatalf("after edits: %v", got)
	}
	// Renaming the series, then deleting it, takes its overrides too.
	e.mustCall(ana, "PATCH", "/calendars/"+cal+"/events/"+ev.Event.ID, map[string]any{"title": "Daily"}, 200, nil)
	e.mustCall(ana, "DELETE", "/calendars/"+cal+"/events/"+ev.Event.ID, nil, 204, nil)
	e.mustCall(ana, "GET", "/calendars/"+cal+"/events?from=2026-03-01&to=2026-04-01", nil, 200, &occ)
	if len(occ.Occurrences) != 0 {
		t.Fatalf("after deleting the series: %+v", occ.Occurrences)
	}
}

const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//EN
X-WR-CALNAME:School
BEGIN:VEVENT
UID:term@school
DTSTART;VALUE=DATE:20260105
DTEND;VALUE=DATE:20260106
SUMMARY:First day of term
END:VEVENT
BEGIN:VEVENT
UID:club@school
DTSTART;TZID=Europe/London:20260106T160000
DURATION:PT1H
RRULE:FREQ=WEEKLY;COUNT=3
EXDATE;TZID=Europe/London:20260113T160000
SUMMARY:Chess club\, room 4
LOCATION:Room 4
END:VEVENT
BEGIN:VEVENT
SUMMARY:Broken
END:VEVENT
END:VCALENDAR
`

func TestICSImportDedupes(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Kids")
	var p calendars.ImportPreview
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import/preview", map[string]string{"ics": sampleICS}, 200, &p)
	if p.Name != "School" || p.Total != 2 || p.New != 2 || len(p.Errors) != 1 {
		t.Fatalf("preview %+v", p)
	}
	var res calendars.ImportResult
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import", map[string]string{"ics": sampleICS}, 200, &res)
	if res.Imported != 2 || res.Skipped != 1 {
		t.Fatalf("import %+v", res)
	}
	updated := strings.Replace(sampleICS, "First day of term", "Term starts", 1)
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import", map[string]string{"ics": updated}, 200, &res)
	if res.Imported != 0 || res.Updated != 2 {
		t.Fatalf("re-import %+v", res)
	}
	var occ occResp
	e.mustCall(ana, "GET", "/calendars/"+cal+"/events?from=2026-01-01&to=2026-02-01&tz=Europe/London", nil, 200, &occ)
	var titles []string
	for _, o := range occ.Occurrences {
		titles = append(titles, o.Start+" "+o.Title)
	}
	want := "[2026-01-05 Term starts 2026-01-06T16:00:00Z Chess club, room 4 2026-01-20T16:00:00Z Chess club, room 4]"
	if fmt.Sprint(titles) != want {
		t.Fatalf("got %v\nwant %s", titles, want)
	}
	// Links to private addresses are refused.
	if got := e.call(ana, "POST", "/calendars/"+cal+"/import/preview", map[string]string{"url": e.srv.URL + "/x.ics"}, nil); got != 400 {
		t.Fatalf("loopback link: %d", got)
	}
}

func TestTrainingCopyWeekAndUndo(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "training", "Marathon")
	var types struct{ ActivityTypes []calendars.ActivityType }
	e.mustCall(ana, "GET", "/calendars/"+cal+"/activity-types", nil, 200, &types)
	if len(types.ActivityTypes) == 0 {
		t.Fatal("no starter activity types")
	}
	run := types.ActivityTypes[0].ID
	base := "/calendars/" + cal + "/sessions"
	for _, s := range []map[string]any{
		{"date": "2026-12-07", "activityTypeId": run, "plan": map[string]any{"distanceKm": 8}},
		{"date": "2026-12-09", "title": "Intervals", "time": "07:00", "activityTypeId": run},
		{"date": "2026-12-12", "title": "Long run", "activityTypeId": run},
	} {
		e.mustCall(ana, "POST", base, s, 201, nil)
	}
	// Something already in week 2 that "replace" will clear.
	e.mustCall(ana, "POST", base, map[string]any{"date": "2026-12-15", "title": "Old"}, 201, nil)
	// Mark one done, so the copy shows it reset.
	var week1 struct{ Sessions []calendars.Session }
	e.mustCall(ana, "GET", base+"?from=2026-12-07&to=2026-12-14", nil, 200, &week1)
	e.mustCall(ana, "PATCH", base+"/"+week1.Sessions[0].ID, map[string]any{"status": "done", "result": map[string]any{"distanceKm": 8.4}}, 200, nil)
	if week1.Sessions[0].Title != "Run" {
		t.Fatalf("untitled session should take its activity's name, got %q", week1.Sessions[0].Title)
	}

	var res calendars.CopyResult
	e.mustCall(ana, "POST", base+"/copy", map[string]any{"from": "2026-12-07", "to": []string{"2026-12-14", "2026-12-21"}, "days": 7, "mode": "replace"}, 200, &res)
	if len(res.Created) != 6 || len(res.Removed) != 1 {
		t.Fatalf("copy %+v", res)
	}
	var later struct{ Sessions []calendars.Session }
	e.mustCall(ana, "GET", base+"?from=2026-12-14&to=2026-12-28", nil, 200, &later)
	var got []string
	for _, s := range later.Sessions {
		got = append(got, s.Date+" "+s.Title+" "+s.Status+" "+s.Time)
	}
	want := "[2026-12-14 Run planned  2026-12-16 Intervals planned 07:00 2026-12-19 Long run planned  2026-12-21 Run planned  2026-12-23 Intervals planned 07:00 2026-12-26 Long run planned ]"
	if fmt.Sprint(got) != want {
		t.Fatalf("copied:\n%v\nwant\n%s", got, want)
	}
	// Copying onto itself is refused.
	e.mustCall(ana, "POST", base+"/copy", map[string]any{"from": "2026-12-07", "to": []string{"2026-12-10"}, "days": 7}, 400, nil)

	// Undo puts things back as they were.
	e.mustCall(ana, "POST", base+"/restore", map[string]any{"delete": res.Created, "restore": res.Removed}, 204, nil)
	e.mustCall(ana, "GET", base+"?from=2026-12-14&to=2026-12-28", nil, 200, &later)
	if len(later.Sessions) != 1 || later.Sessions[0].Title != "Old" {
		t.Fatalf("after undo %+v", later.Sessions)
	}
}

func TestCycleSummaryAndOverview(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cyc := e.create(ana, "cycle", "Ana")
	gen := e.create(ana, "general", "Family")
	var sum calendars.CycleSummary
	e.mustCall(ana, "GET", "/calendars/"+cyc+"/cycle/summary", nil, 200, &sum)
	if sum.Enough || sum.NeedPeriods != 2 {
		t.Fatalf("empty summary %+v", sum)
	}
	today := sum.Today
	// Periods 70, 42 and 14 days ago: 28-day cycles, the next due in 14.
	for _, ago := range []int{70, 42, 14} {
		for i := 0; i < 4; i++ {
			day := shift(t, today, -ago+i)
			e.mustCall(ana, "PUT", "/calendars/"+cyc+"/cycle/days/"+day, map[string]string{"flow": "medium"}, 204, nil)
		}
	}
	e.mustCall(ana, "GET", "/calendars/"+cyc+"/cycle/summary", nil, 200, &sum)
	if !sum.Enough || sum.MeanCycle != 28 || len(sum.Predictions) != 3 || sum.Predictions[0].Start != shift(t, today, 14) {
		t.Fatalf("summary %+v", sum)
	}
	e.mustCall(ana, "POST", "/calendars/"+gen+"/events", map[string]any{"title": "Dinner", "allDay": true, "start": shift(t, today, 1)}, 201, nil)

	q := "/overview?from=" + shift(t, today, -20) + "&to=" + shift(t, today, 40)
	var ov struct{ Items []calendars.Item }
	e.mustCall(ana, "GET", q, nil, 200, &ov)
	layers := map[string]int{}
	for _, it := range ov.Items {
		layers[it.Kind+"/"+it.Layer]++
	}
	if layers["cycle/period"] != 4 || layers["cycle/predicted"] < 1 || layers["general/event"] != 1 || layers["cycle/fertile"] < 1 {
		t.Fatalf("overview layers %v", layers)
	}
	// Switching a calendar off the Overview drops it.
	e.mustCall(ana, "PATCH", "/calendars/"+cyc+"/me", map[string]any{"inOverview": false}, 200, nil)
	e.mustCall(ana, "GET", q, nil, 200, &ov)
	if len(ov.Items) != 1 {
		t.Fatalf("with cycle hidden: %+v", ov.Items)
	}
	// Clearing a day removes it.
	e.mustCall(ana, "PUT", "/calendars/"+cyc+"/cycle/days/"+shift(t, today, -14), map[string]string{"flow": "none"}, 204, nil)
	var days struct{ Days []calendars.CycleDay }
	e.mustCall(ana, "GET", "/calendars/"+cyc+"/cycle/days", nil, 200, &days)
	if len(days.Days) != 11 {
		t.Fatalf("days %d", len(days.Days))
	}
}

func shift(t *testing.T, date string, n int) string {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatal(err)
	}
	return d.AddDate(0, 0, n).Format("2006-01-02")
}

func TestRepeatRulesAreBounded(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Home")
	base := "/calendars/" + cal + "/events"
	// More often than daily is refused, from the app or a file.
	e.mustCall(ana, "POST", base, map[string]any{"title": "x", "start": "2000-01-01T00:00:00Z", "rrule": "FREQ=SECONDLY"}, 400, nil)
	var p calendars.ImportPreview
	ics := strings.Replace(sampleICS, "RRULE:FREQ=WEEKLY;COUNT=3", "RRULE:FREQ=MINUTELY", 1)
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import/preview", map[string]string{"ics": ics}, 200, &p)
	if p.Total != 1 || len(p.Errors) != 2 {
		t.Fatalf("minutely import: %+v", p)
	}
	// A daily series from long ago still expands today, quickly.
	e.mustCall(ana, "POST", base, map[string]any{"title": "Pills", "allDay": true, "start": "2000-01-01", "rrule": "FREQ=DAILY"}, 201, nil)
	var occ occResp
	e.mustCall(ana, "GET", base+"?from=2026-03-01&to=2026-03-08", nil, 200, &occ)
	if len(occ.Occurrences) != 7 {
		t.Fatalf("daily since 2000: %d occurrences", len(occ.Occurrences))
	}
}

func TestEditingSeriesKeepsItsExceptions(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Work")
	var ev struct{ Event calendars.Event }
	e.mustCall(ana, "POST", "/calendars/"+cal+"/events", map[string]any{
		"title": "Standup", "start": "2026-03-02T09:00:00-05:00", "end": "2026-03-02T09:15:00-05:00",
		"tzid": "America/New_York", "rrule": "FREQ=WEEKLY;COUNT=4",
	}, 201, &ev)
	path := "/calendars/" + cal + "/events/" + ev.Event.ID
	q := "/calendars/" + cal + "/events?from=2026-03-01&to=2026-04-01&tz=America/New_York"
	var occ occResp
	e.mustCall(ana, "GET", q, nil, 200, &occ)
	// Delete the 2nd, rename the 3rd only.
	e.mustCall(ana, "DELETE", path+"?occurrence="+occ.Occurrences[1].Occurrence, nil, 204, nil)
	e.mustCall(ana, "PATCH", path+"?occurrence="+occ.Occurrences[2].Occurrence, map[string]any{"title": "Retro"}, 200, nil)

	// Move them all an hour later (across the DST change on 8 March).
	e.mustCall(ana, "PATCH", path, map[string]any{"start": "2026-03-02T10:00:00-05:00", "end": "2026-03-02T10:15:00-05:00"}, 200, nil)
	e.mustCall(ana, "GET", q, nil, 200, &occ)
	var got []string
	for _, o := range occ.Occurrences {
		got = append(got, o.Start+" "+o.Title)
	}
	// The deleted one stays deleted and the renamed one isn't doubled. The
	// override kept its own time (9:00), as it was edited on its own.
	want := "[2026-03-02T15:00:00Z Standup 2026-03-16T13:00:00Z Retro 2026-03-23T14:00:00Z Standup]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got  %v\nwant %s", got, want)
	}
}

func TestReimportKeepsDeletedOccurrences(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Kids")
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import", map[string]string{"ics": sampleICS}, 200, nil)
	q := "/calendars/" + cal + "/events?from=2026-01-01&to=2026-02-01&tz=Europe/London"
	var occ occResp
	e.mustCall(ana, "GET", q, nil, 200, &occ)
	var club calendars.Occurrence
	for _, o := range occ.Occurrences {
		if o.Recurring {
			club = o
			break
		}
	}
	e.mustCall(ana, "DELETE", "/calendars/"+cal+"/events/"+club.EventID+"?occurrence="+club.Occurrence, nil, 204, nil)
	e.mustCall(ana, "POST", "/calendars/"+cal+"/import", map[string]string{"ics": sampleICS}, 200, nil)
	e.mustCall(ana, "GET", q, nil, 200, &occ)
	for _, o := range occ.Occurrences {
		if o.Occurrence == club.Occurrence {
			t.Fatalf("deleted occurrence came back after re-import: %+v", o)
		}
	}
}

func TestCopyRefusesOverlappingTargets(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "training", "Plan")
	e.mustCall(ana, "POST", "/calendars/"+cal+"/sessions/copy", map[string]any{
		"from": "2026-01-01", "to": []string{"2026-01-15", "2026-01-22"}, "days": 14, "mode": "replace",
	}, 400, nil)
}

func TestInvitesAreLimited(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Home")
	for i := 0; i < calendars.InvitesPerDay; i++ {
		e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": fmt.Sprintf("p%d@x.co", i)}, 201, nil)
	}
	// Re-inviting someone already invited is fine; a new address isn't.
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": "p0@x.co"}, 201, nil)
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": "one-too-many@x.co"}, 400, nil)
}

func TestOverviewShowsSessionAmounts(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "training", "Plan")
	var types struct{ ActivityTypes []calendars.ActivityType }
	e.mustCall(ana, "GET", "/calendars/"+cal+"/activity-types", nil, 200, &types)
	run := types.ActivityTypes[0] // Run
	base := "/calendars/" + cal + "/sessions"
	var s struct{ Session calendars.Session }
	e.mustCall(ana, "POST", base, map[string]any{"date": "2026-12-07", "activityTypeId": run.ID, "plan": map[string]any{"distanceKm": 8, "durationMin": 45}}, 201, &s)
	e.mustCall(ana, "POST", base, map[string]any{"date": "2026-12-08", "title": "Gym", "plan": map[string]any{"durationMin": 60}}, 201, nil)

	var ov struct{ Items []calendars.Item }
	e.mustCall(ana, "GET", "/overview?from=2026-12-07&to=2026-12-09", nil, 200, &ov)
	if len(ov.Items) != 2 {
		t.Fatalf("items %+v", ov.Items)
	}
	first := ov.Items[0]
	if first.Icon != "run" || first.DistanceKm == nil || *first.DistanceKm != 8 || first.ActivityColor != run.Color {
		t.Fatalf("run item %+v", first)
	}
	if gym := ov.Items[1]; gym.Icon != "" || gym.DistanceKm != nil || gym.DurationMin == nil || *gym.DurationMin != 60 {
		t.Fatalf("gym item %+v", gym)
	}
	// Once done, what was actually run is shown.
	e.mustCall(ana, "PATCH", base+"/"+s.Session.ID, map[string]any{"status": "done", "result": map[string]any{"distanceKm": 8.4}}, 200, nil)
	e.mustCall(ana, "GET", "/overview?from=2026-12-07&to=2026-12-08", nil, 200, &ov)
	if d := ov.Items[0].DistanceKm; d == nil || *d != 8.4 {
		t.Fatalf("done item distance %v", d)
	}
}

func TestEventColours(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Home") // #336699
	base := "/calendars/" + cal + "/events"
	e.mustCall(ana, "POST", base, map[string]any{"title": "Red", "allDay": true, "start": "2026-05-01", "color": "#e03131"}, 201, nil)
	e.mustCall(ana, "POST", base, map[string]any{"title": "Plain", "allDay": true, "start": "2026-05-01"}, 201, nil)
	e.mustCall(ana, "POST", base, map[string]any{"title": "Bad", "allDay": true, "start": "2026-05-01", "color": "red"}, 400, nil)

	var ov struct{ Items []calendars.Item }
	e.mustCall(ana, "GET", "/overview?from=2026-05-01&to=2026-05-02", nil, 200, &ov)
	got := map[string]string{}
	for _, it := range ov.Items {
		got[it.Title] = it.Color
	}
	if got["Red"] != "#e03131" || got["Plain"] != "#336699" {
		t.Fatalf("overview colours %v", got)
	}
}
