package calendars

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/google/uuid"
)

// Subscribed calendars follow an iCal link (Google's secret address, an
// Outlook published calendar, any webcal feed). The server re-fetches each
// link on its own schedule and makes the calendar's events match the feed
// exactly; nobody edits them in the app (their write routes aren't routed for
// the kind). The link is a secret: it lives in calendar_feeds, only whoever
// added it is sent it, and errors and logs only ever name its host.
//
// The rules that keep a feed's trouble from costing anyone their events:
//   - a failed download, an error answer or a file that won't parse changes
//     nothing but the feed's status;
//   - one empty feed doesn't empty a calendar that has events; a second one
//     in a row does (a provider may briefly serve an empty file).

const (
	feedEvery      = 15 * time.Minute // after a successful sync
	feedGoneEvery  = 24 * time.Hour   // a link that answers 401/403/404/410
	feedMaxBackoff = 6 * time.Hour    // after passing failures, doubling from feedEvery
	feedRefreshGap = time.Minute      // between "Refresh now"s of one calendar
	feedTimeout    = 30 * time.Second
	maxFeedBytes   = 20 << 20
	maxFeedEvents  = 20000
	// How many feeds one pass of the sync job takes on (it runs every minute).
	feedBatch           = 50
	feedPreviewsPerHour = 30
	maxPreviewErrors    = 20
)

// ErrTooSoon: a refresh or preview asked for again too quickly.
var ErrTooSoon = errors.New("too soon")

// Feed is a subscribed calendar's link and how syncing it is going, as one
// member sees it: only whoever added the link gets URL.
type Feed struct {
	Status       string  `json:"status"` // ok, failing, broken
	LastError    string  `json:"lastError"`
	LastSyncedAt *string `json:"lastSyncedAt"`
	NextSyncAt   string  `json:"nextSyncAt"`
	EventCount   int     `json:"eventCount"`
	Host         string  `json:"host"`
	URL          string  `json:"url,omitempty"`
	Mine         bool    `json:"mine"` // you added the link
	AddedByName  string  `json:"addedByName"`
}

// feedRow is what syncing needs from calendar_feeds.
type feedRow struct {
	calendarID, url, tz, status  string
	etag, lastModified, bodyHash string
	failures, emptyFetches       int
}

func (s *Service) loadFeed(ctx context.Context, calendarID string) (*feedRow, error) {
	f := feedRow{calendarID: calendarID}
	err := s.db.QueryRowContext(ctx, `SELECT url, tz, status, etag, last_modified, body_hash, failures, empty_fetches
		FROM calendar_feeds WHERE calendar_id = ?`, calendarID).
		Scan(&f.url, &f.tz, &f.status, &f.etag, &f.lastModified, &f.bodyHash, &f.failures, &f.emptyFetches)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// Feed is calendarID's link and sync status as userID (a member) sees it.
func (s *Service) Feed(ctx context.Context, calendarID, userID string) (*Feed, error) {
	var f Feed
	var url string
	var addedBy, synced sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT f.url, f.added_by, COALESCE(u.display_name, ''), f.status, f.last_error,
			f.last_synced_at, f.next_sync_at,
			(SELECT COUNT(*) FROM events e WHERE e.calendar_id = f.calendar_id AND e.recurrence_id = '')
		FROM calendar_feeds f LEFT JOIN users u ON u.id = f.added_by WHERE f.calendar_id = ?`, calendarID).
		Scan(&url, &addedBy, &f.AddedByName, &f.Status, &f.LastError, &synced, &f.NextSyncAt, &f.EventCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if synced.Valid {
		f.LastSyncedAt = &synced.String
	}
	f.Host = linkHost(url)
	if addedBy.Valid && addedBy.String == userID {
		f.Mine, f.URL = true, url
	}
	return &f, nil
}

// --- Fetching and mirroring ---

// fetchedFeed is a downloaded and parsed feed (just the response, if it
// answered "not modified").
type fetchedFeed struct {
	res    *FetchResponse
	name   string
	events []*Event
	errs   []string
}

func (s *Service) fetchFeed(ctx context.Context, calendarID, url, tz, etag, lastModified string) (*fetchedFeed, error) {
	res, err := s.fetch(ctx, FetchRequest{URL: url, ETag: etag, LastModified: lastModified, MaxBytes: maxFeedBytes, Timeout: feedTimeout})
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return &fetchedFeed{res: res}, nil
	}
	name, events, errs, err := parseICS(res.Body, calendarID, loadLoc(tz), maxFeedEvents)
	if err != nil {
		// Often a sign-in or error page served with a 200.
		return nil, &FetchError{Msg: "the link didn't give a calendar file"}
	}
	return &fetchedFeed{res: res, name: name, events: events, errs: errs}, nil
}

func bodyHash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// backoff is how long to wait after the nth passing failure in a row.
func backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		return feedMaxBackoff
	}
	return min(feedEvery<<(n-1), feedMaxBackoff)
}

// applyFeed records how fetching f went and, on success, makes the
// calendar's events match the feed. It reports whether the events changed and
// whether the feed's status did.
func (s *Service) applyFeed(ctx context.Context, f *feedRow, got *fetchedFeed, fetchErr error) (eventsChanged, statusChanged bool, err error) {
	now := s.now()
	if fetchErr != nil {
		msg, status := "couldn't download that link", "failing"
		var fe *FetchError
		var inv *InvalidError
		switch {
		case errors.As(fetchErr, &fe):
			msg = fe.Msg
			if fe.Gone() {
				status = "broken"
			}
		case errors.As(fetchErr, &inv):
			msg = inv.Msg
		}
		next := now.Add(backoff(f.failures + 1))
		if status == "broken" {
			next = now.Add(feedGoneEvery)
		}
		_, err := s.db.ExecContext(ctx, `UPDATE calendar_feeds SET status = ?, last_error = ?, failures = failures + 1,
			next_sync_at = ?, updated_at = ? WHERE calendar_id = ?`, status, msg, stamp(next), stamp(now), f.calendarID)
		return false, status != f.status, err
	}

	etag, lastModified, hash := got.res.ETag, got.res.LastModified, f.bodyHash
	unchanged := got.res.NotModified
	if !unchanged {
		hash = bodyHash(got.res.Body)
		unchanged = hash == f.bodyHash
	}
	emptyFetches := 0
	var existing map[string]*Event
	if !unchanged {
		// Read before the transaction: the pool has a single connection.
		if existing, err = s.existing(ctx, f.calendarID); err != nil {
			return false, false, err
		}
		if len(got.events) == 0 && len(existing) > 0 && f.emptyFetches == 0 {
			// The first empty feed is held back. The old hash and validators
			// stay, so the next sync downloads it again rather than skipping
			// it as unchanged.
			unchanged, emptyFetches = true, 1
			etag, lastModified, hash = f.etag, f.lastModified, f.bodyHash
		}
	}
	if unchanged && etag == "" && lastModified == "" {
		// The same file without validators this time: keep the old ones.
		etag, lastModified = f.etag, f.lastModified
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	if !unchanged {
		if eventsChanged, err = s.mirror(ctx, tx, f.calendarID, existing, got.events); err != nil {
			return false, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_feeds SET status = 'ok', last_error = '', failures = 0,
		empty_fetches = ?, etag = ?, last_modified = ?, body_hash = ?, last_synced_at = ?, next_sync_at = ?, updated_at = ?
		WHERE calendar_id = ?`, emptyFetches, etag, lastModified, hash, stamp(now), stamp(now.Add(feedEvery)), stamp(now),
		f.calendarID); err != nil {
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return eventsChanged, f.status != "ok", nil
}

// mirror makes calendarID's events exactly the feed's: new ones added,
// changed ones updated (matched by UID and RECURRENCE-ID, as ICS does) and
// the rest deleted. Unchanged events aren't touched, so their updatedAt
// stays put.
func (s *Service) mirror(ctx context.Context, tx *sql.Tx, calendarID string, existing map[string]*Event, events []*Event) (changed bool, err error) {
	seen := map[string]bool{}
	for _, e := range events {
		key := e.UID + "|" + e.RecurrenceID
		if seen[key] {
			continue // the feed repeats itself; the first one wins
		}
		seen[key] = true
		old, ok := existing[key]
		if !ok {
			e.CalendarID = calendarID
			if err := s.insertEvent(ctx, tx, e, ""); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if sameEvent(old, e) {
			continue
		}
		e.ID, e.Color = old.ID, old.Color
		if err := s.saveEvent(ctx, tx, e, ""); err != nil {
			return false, err
		}
		changed = true
	}
	for key, old := range existing {
		if seen[key] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, old.ID); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func sameEvent(a, b *Event) bool {
	return a.Title == b.Title && a.Description == b.Description && a.Location == b.Location && a.URL == b.URL &&
		a.AllDay == b.AllDay && a.Start == b.Start && a.End == b.End && a.TZID == b.TZID && a.RRule == b.RRule &&
		a.Cancelled == b.Cancelled && slices.Equal(a.ExDates, b.ExDates)
}

// syncFeed fetches calendarID's feed and applies it.
func (s *Service) syncFeed(ctx context.Context, calendarID string) (eventsChanged, statusChanged bool, err error) {
	defer s.feedLocks.lock(calendarID)()
	f, err := s.loadFeed(ctx, calendarID)
	if err != nil {
		return false, false, err
	}
	got, fetchErr := s.fetchFeed(ctx, calendarID, f.url, f.tz, f.etag, f.lastModified)
	return s.applyFeed(ctx, f, got, fetchErr)
}

// syncAndNotify syncs a feed and tells its members' apps what changed.
func (s *Service) syncAndNotify(ctx context.Context, calendarID string) error {
	eventsChanged, statusChanged, err := s.syncFeed(ctx, calendarID)
	if err != nil {
		return err
	}
	switch {
	case statusChanged:
		// The status shows in the calendar list; "calendar" reloads that and
		// the calendar's events.
		s.notifyCalendar(ctx, calendarID, "calendar")
	case eventsChanged:
		s.notifyCalendar(ctx, calendarID, "events")
	}
	return nil
}

// notifyCalendar tells calendarID's members about a change the server made
// itself (so there's no By).
func (s *Service) notifyCalendar(ctx context.Context, calendarID, area string) {
	s.send(s.memberIDs(ctx, calendarID), live.Message{Type: "changed", CalendarID: calendarID, Area: area})
}

// UseClock replaces the service's clock (tests only).
func (s *Service) UseClock(now func() time.Time) { s.now = now }

// --- The sync job ---

// RunFeedSync syncs every feed that's due, once a minute, until ctx ends.
func (s *Service) RunFeedSync(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.SyncDueFeeds(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SyncDueFeeds syncs the feeds whose next sync has come, oldest first, and
// returns how many it tried.
func (s *Service) SyncDueFeeds(ctx context.Context) int {
	// Collected before syncing: the pool has one connection, which an open
	// result set would hold.
	rows, err := s.db.QueryContext(ctx, `SELECT calendar_id FROM calendar_feeds WHERE next_sync_at <= ?
		ORDER BY next_sync_at LIMIT ?`, stamp(s.now()), feedBatch)
	if err != nil {
		log.Printf("feeds: %v", err)
		return 0
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		// Feed trouble is recorded on the feed; only the database's own
		// errors come back here. Logged by calendar id, never the link.
		if err := s.syncAndNotify(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
			log.Printf("feeds: calendar %s: %v", id, err)
		}
	}
	return len(ids)
}

// --- What the API does ---

// PreviewSubscription fetches a link to show what subscribing would bring in.
func (s *Service) PreviewSubscription(ctx context.Context, userID, url, tz string) (*ImportPreview, error) {
	if !s.feedPreviews.Allow(userID) {
		return nil, ErrTooSoon
	}
	got, err := s.fetchFeed(ctx, "", url, tz, "", "")
	if err != nil {
		return nil, err
	}
	p := &ImportPreview{Name: got.name, Total: len(got.events), New: len(got.events), Errors: got.errs, Sample: []*Event{}}
	if len(p.Errors) > maxPreviewErrors {
		p.Errors = p.Errors[:maxPreviewErrors]
	}
	for _, e := range got.events {
		if len(p.Sample) == 50 {
			break
		}
		p.Sample = append(p.Sample, e)
	}
	return p, nil
}

// CreateSubscribed makes a subscribed calendar following url, with userID its
// first member. The link is fetched first, so a bad one makes nothing, and
// the calendar opens with the feed's events already in it.
func (s *Service) CreateSubscribed(ctx context.Context, userID, name, color, url, tz string) (*Calendar, error) {
	name, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	if !colorRe.MatchString(color) {
		return nil, invalid("colour must look like #a1b2c3")
	}
	id := uuid.NewString()
	got, err := s.fetchFeed(ctx, id, url, tz, "", "")
	if err != nil {
		return nil, err
	}
	defer s.feedLocks.lock(id)()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO calendars (id, kind, name, color, settings, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?, ?, ?)`, id, Subscribed, name, color, userID, now, now); err != nil {
		return nil, err
	}
	if err := addMember(ctx, tx, id, userID, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_feeds (calendar_id, url, tz, added_by, next_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, normalizedLink(url), validTZ(tz), userID, now, now, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// Should this fail, the calendar is due for a sync now anyway.
	if f, err := s.loadFeed(ctx, id); err == nil {
		if _, _, err := s.applyFeed(ctx, f, got, nil); err != nil {
			log.Printf("feeds: calendar %s: first sync: %v", id, err)
		}
	}
	return s.Get(ctx, id, userID)
}

// ReplaceFeedLink points a subscribed calendar at a new link, as after the
// old secret address was reset. Any member may (whoever added the old one may
// have left); they become the one who added it. The new link is fetched
// first, so a bad one changes nothing.
func (s *Service) ReplaceFeedLink(ctx context.Context, calendarID, userID, url, tz string) error {
	got, err := s.fetchFeed(ctx, calendarID, url, tz, "", "")
	if err != nil {
		return err
	}
	defer s.feedLocks.lock(calendarID)()
	if _, err := s.db.ExecContext(ctx, `UPDATE calendar_feeds SET url = ?, tz = ?, added_by = ?, etag = '', last_modified = '',
		body_hash = '', empty_fetches = 0, failures = 0, updated_at = ? WHERE calendar_id = ?`,
		normalizedLink(url), validTZ(tz), userID, stamp(s.now()), calendarID); err != nil {
		return err
	}
	f, err := s.loadFeed(ctx, calendarID)
	if err != nil {
		return err
	}
	_, _, err = s.applyFeed(ctx, f, got, nil)
	return err
}

// RefreshFeed syncs a feed now, at most once a minute per calendar. Trouble
// with the link isn't an error here: it's in the feed's status.
func (s *Service) RefreshFeed(ctx context.Context, calendarID string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `UPDATE calendar_feeds SET last_refresh_at = ?
		WHERE calendar_id = ? AND (last_refresh_at IS NULL OR last_refresh_at <= ?)`,
		stamp(now), calendarID, stamp(now.Add(-feedRefreshGap)))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTooSoon
	}
	_, _, err = s.syncFeed(ctx, calendarID)
	return err
}

// normalizedLink is the link as stored: trimmed, otherwise as given (a
// webcal link keeps its scheme, so fetching can fall back to plain http).
func normalizedLink(url string) string { return strings.TrimSpace(url) }

func validTZ(tz string) string {
	if tz == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}

// keyedMutex serializes work per key: one sync of a feed at a time, whether
// the job, a refresh or a new link started it.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	sync.Mutex
	users int
}

// lock takes key's lock and returns the function that releases it.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l := k.locks[key]
	if l == nil {
		l = &keyedLock{}
		k.locks[key] = l
	}
	l.users++
	k.mu.Unlock()

	l.Lock()
	return func() {
		l.Unlock()
		k.mu.Lock()
		if l.users--; l.users == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
