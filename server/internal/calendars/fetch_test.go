package calendars

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchICS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cal.ics":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Write([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
		case "/big.ics":
			w.Write([]byte(strings.Repeat("x", 2000)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// The real client refuses loopback addresses, as it should.
	saved := fetchClient
	fetchClient = srv.Client()
	defer func() { fetchClient = saved }()
	ctx := context.Background()

	res, err := fetchICS(ctx, FetchRequest{URL: srv.URL + "/cal.ics"})
	if err != nil || res.ETag != `"v1"` || !strings.HasPrefix(res.Body, "BEGIN:VCALENDAR") {
		t.Fatalf("first fetch: %+v, %v", res, err)
	}
	res, err = fetchICS(ctx, FetchRequest{URL: srv.URL + "/cal.ics", ETag: `"v1"`})
	if err != nil || !res.NotModified || res.ETag != `"v1"` {
		t.Fatalf("conditional fetch: %+v, %v", res, err)
	}

	secret := srv.URL + "/private-s3cr3t.ics"
	_, err = fetchICS(ctx, FetchRequest{URL: secret})
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Status != 404 || !fe.Gone() || strings.Contains(fe.Msg, "s3cr3t") {
		t.Fatalf("404: %#v", err)
	}
	if _, err := fetchICS(ctx, FetchRequest{URL: srv.URL + "/big.ics", MaxBytes: 1000}); !errors.As(err, &fe) || fe.Status != 0 {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := fetchICS(ctx, FetchRequest{URL: "ftp://example.com/x.ics"}); err == nil {
		t.Fatal("an ftp link was fetched")
	}
	if h := linkHost("webcal://calendar.example.com/private-s3cr3t/basic.ics"); h != "calendar.example.com" {
		t.Fatalf("host %q", h)
	}
	// And the real client still won't reach this machine.
	fetchClient = saved
	if _, err := fetchICS(ctx, FetchRequest{URL: srv.URL + "/cal.ics"}); !errors.As(err, &fe) || !strings.Contains(fe.Msg, "public internet") {
		t.Fatalf("loopback with the real client: %v", err)
	}
}
