package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/calendars"
	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/gorilla/websocket"
)

// listen opens the live socket as token and returns its messages.
func (e *env) listen(token string) <-chan live.Message {
	e.t.Helper()
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/live"
	ws, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		e.t.Fatalf("dial live: %v", err)
	}
	e.t.Cleanup(func() { ws.Close() })
	out := make(chan live.Message, 32)
	go func() {
		for {
			var m live.Message
			if err := ws.ReadJSON(&m); err != nil {
				close(out)
				return
			}
			out <- m
		}
	}()
	if m := next(e.t, out); m.Type != "hello" {
		e.t.Fatalf("first message %+v, want hello", m)
	}
	return out
}

func next(t *testing.T, ch <-chan live.Message) live.Message {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no live message")
		return live.Message{}
	}
}

func quiet(t *testing.T, ch <-chan live.Message) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("unexpected live message %+v", m)
	case <-time.After(150 * time.Millisecond):
	}
}

// callAs is call with the app's X-Live-Client id.
func (e *env) callAs(token, client, method, path string, body any) int {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Live-Client", client)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestLiveTellsMembersOnly(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	bo := e.signUp("bo@x.co", "Bo")
	cy := e.signUp("cy@x.co", "Cy")
	cal := e.create(ana, "general", "Ours")

	boLive := e.listen(bo)
	// Bo is invited: his Inbox changes.
	var inv struct{ Invite calendars.Invite }
	e.mustCall(ana, "POST", "/calendars/"+cal+"/invites", map[string]string{"email": "bo@x.co"}, 201, &inv)
	if m := next(t, boLive); m.Type != "inbox" {
		t.Fatalf("invite: %+v", m)
	}

	anaLive := e.listen(ana)
	cyLive := e.listen(cy)
	e.mustCall(bo, "POST", "/invites/"+inv.Invite.ID+"/accept", nil, 200, nil)
	if m := next(t, anaLive); m.Type != "changed" || m.Area != "members" || m.ByName != "Bo" {
		t.Fatalf("accept, to Ana: %+v", m)
	}

	// Ana adds an event from her phone "p1": Bo hears it; Ana's own socket
	// hears it marked with her origin; Cy, not a member, hears nothing.
	for len(boLive) > 0 {
		<-boLive
	}
	if got := e.callAs(ana, "p1", "POST", "/calendars/"+cal+"/events", map[string]any{"title": "Dinner", "allDay": true, "start": "2026-05-01"}); got != 201 {
		t.Fatalf("create: %d", got)
	}
	m := next(t, boLive)
	if m.Type != "changed" || m.CalendarID != cal || m.Area != "events" || m.ByName != "Ana" || m.Origin != "p1" {
		t.Fatalf("to Bo: %+v", m)
	}
	if m := next(t, anaLive); m.Origin != "p1" {
		t.Fatalf("to Ana: %+v", m)
	}
	quiet(t, cyLive)

	// Reads tell nobody.
	e.mustCall(ana, "GET", "/calendars/"+cal+"/events?from=2026-05-01&to=2026-05-02", nil, 200, nil)
	quiet(t, boLive)

	// Bo leaves: Ana hears the member list changed; Bo's list changed.
	e.mustCall(bo, "DELETE", "/calendars/"+cal+"/me", nil, 204, nil)
	if m := next(t, boLive); m.Type != "calendars" {
		t.Fatalf("leave, to Bo: %+v", m)
	}
	if m := next(t, anaLive); m.Area != "members" {
		t.Fatalf("leave, to Ana: %+v", m)
	}
	// After leaving, Bo hears nothing more about it.
	e.mustCall(ana, "POST", "/calendars/"+cal+"/events", map[string]any{"title": "Lunch", "allDay": true, "start": "2026-05-02"}, 201, nil)
	next(t, anaLive)
	quiet(t, boLive)
}

func TestSaveRefusesToOverwriteNewerChange(t *testing.T) {
	e := newEnv(t)
	ana := e.signUp("ana@x.co", "Ana")
	cal := e.create(ana, "general", "Home")
	var ev struct{ Event calendars.Event }
	e.mustCall(ana, "POST", "/calendars/"+cal+"/events", map[string]any{"title": "Dinner", "allDay": true, "start": "2026-05-01"}, 201, &ev)
	base := ev.Event.UpdatedAt
	path := "/calendars/" + cal + "/events/" + ev.Event.ID

	// Someone saves first...
	time.Sleep(5 * time.Millisecond)
	e.mustCall(ana, "PATCH", path, map[string]any{"title": "Dinner at 7", "baseUpdatedAt": base}, 200, nil)
	// ...so a save based on the old version is refused, with what's there now.
	var conflict struct {
		Conflict bool
		By       string
		Current  calendars.Event
	}
	e.mustCall(ana, "PATCH", path, map[string]any{"title": "Dinner at 8", "baseUpdatedAt": base}, 409, &conflict)
	if !conflict.Conflict || conflict.Current.Title != "Dinner at 7" || conflict.By != "Ana" {
		t.Fatalf("conflict %+v", conflict)
	}
	// Without a base it overwrites, as asked.
	e.mustCall(ana, "PATCH", path, map[string]any{"title": "Dinner at 8"}, 200, nil)

	// Sessions too.
	tcal := e.create(ana, "training", "Plan")
	var s struct{ Session calendars.Session }
	e.mustCall(ana, "POST", "/calendars/"+tcal+"/sessions", map[string]any{"date": "2026-05-01", "title": "Run"}, 201, &s)
	time.Sleep(5 * time.Millisecond)
	e.mustCall(ana, "PATCH", "/calendars/"+tcal+"/sessions/"+s.Session.ID, map[string]any{"title": "Long run"}, 200, nil)
	e.mustCall(ana, "PATCH", "/calendars/"+tcal+"/sessions/"+s.Session.ID, map[string]any{"title": "Tempo", "baseUpdatedAt": s.Session.UpdatedAt}, 409, nil)
}
