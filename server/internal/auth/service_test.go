package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/testdb"
)

type fakeMailer struct {
	codes map[string]string
}

func (m *fakeMailer) SendLoginCode(to, code string) error {
	m.codes[to] = code
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeMailer, *time.Time) {
	t.Helper()
	mailer := &fakeMailer{codes: map[string]string{}}
	store := NewStore(testdb.Open(t))
	svc := NewService(store, mailer)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, mailer, &now
}

func TestSignUpNeedsNameThenSignsIn(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)

	if err := svc.RequestCode(ctx, " Ada@Example.com ", ""); err != nil {
		t.Fatal(err)
	}
	code := mailer.codes["ada@example.com"]

	if _, _, err := svc.Verify(ctx, "ada@example.com", code, ""); !errors.Is(err, ErrNeedsName) {
		t.Fatalf("want ErrNeedsName, got %v", err)
	}
	// The same code still works once a name is supplied.
	user, token, err := svc.Verify(ctx, "ada@example.com", code, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Ada" || token == "" {
		t.Fatalf("unexpected user %+v token %q", user, token)
	}
	if id, err := svc.Authenticate(ctx, token); err != nil || id != user.ID {
		t.Fatalf("authenticate: %v %q", err, id)
	}

	// The code can't be reused.
	if _, _, err := svc.Verify(ctx, "ada@example.com", code, "Ada"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reuse: want ErrInvalidCode, got %v", err)
	}

	// An existing account signs in without a name.
	if err := svc.RequestCode(ctx, "ada@example.com", ""); err != nil {
		t.Fatal(err)
	}
	again, _, err := svc.Verify(ctx, "ada@example.com", mailer.codes["ada@example.com"], "")
	if err != nil || again.ID != user.ID {
		t.Fatalf("second sign-in: %v %+v", err, again)
	}
}

func TestCodeLocksAfterTooManyWrongGuesses(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)
	_ = svc.RequestCode(ctx, "a@b.co", "")
	code := mailer.codes["a@b.co"]

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < maxCodeAttempts; i++ {
		if _, _, err := svc.Verify(ctx, "a@b.co", wrong, "A"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, _, err := svc.Verify(ctx, "a@b.co", code, "A"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("right code after lockout: want ErrInvalidCode, got %v", err)
	}
}

func TestCodeExpiresAndNewCodeRetiresOld(t *testing.T) {
	ctx := context.Background()
	svc, mailer, now := newTestService(t)

	_ = svc.RequestCode(ctx, "a@b.co", "")
	first := mailer.codes["a@b.co"]
	*now = now.Add(time.Second)
	_ = svc.RequestCode(ctx, "a@b.co", "")
	second := mailer.codes["a@b.co"]
	if first != second {
		if _, _, err := svc.Verify(ctx, "a@b.co", first, "A"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("old code: want ErrInvalidCode, got %v", err)
		}
	}

	*now = now.Add(codeTTL + time.Second)
	if _, _, err := svc.Verify(ctx, "a@b.co", second, "A"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expired code: want ErrInvalidCode, got %v", err)
	}
}

func signUp(t *testing.T, svc *Service, mailer *fakeMailer, email, name string) string {
	t.Helper()
	ctx := context.Background()
	if err := svc.RequestCode(ctx, email, ""); err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.Verify(ctx, email, mailer.codes[email], name)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestSessionSlidesAndExpiresWhenIdle(t *testing.T) {
	ctx := context.Background()
	svc, mailer, now := newTestService(t)
	token := signUp(t, svc, mailer, "a@b.co", "A")

	// Used every 60 days: never expires.
	for i := 0; i < 5; i++ {
		*now = now.Add(60 * 24 * time.Hour)
		if _, err := svc.Authenticate(ctx, token); err != nil {
			t.Fatalf("after %d active periods: %v", i+1, err)
		}
	}

	// Idle past the TTL: signed out.
	*now = now.Add(sessionIdleTTL + time.Hour)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("idle session: want ErrInvalidSession, got %v", err)
	}
}

func TestLogoutEndsOnlyThatSession(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)
	phone := signUp(t, svc, mailer, "a@b.co", "A")
	_ = svc.RequestCode(ctx, "a@b.co", "")
	_, tablet, err := svc.Verify(ctx, "a@b.co", mailer.codes["a@b.co"], "")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Logout(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, phone); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("logged-out session: want ErrInvalidSession, got %v", err)
	}
	if _, err := svc.Authenticate(ctx, tablet); err != nil {
		t.Fatalf("other session: %v", err)
	}
}

func TestUserCapStopsNewSignUps(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)
	if err := svc.store.SetUserCap(ctx, 2); err != nil {
		t.Fatal(err)
	}

	signUp(t, svc, mailer, "ada@example.com", "Ada")
	// Bob gets a code while there's room, but Cy takes the last place
	// before he uses it.
	if err := svc.RequestCode(ctx, "a@b.co", ""); err != nil {
		t.Fatal(err)
	}
	signUp(t, svc, mailer, "cy@example.com", "Cy")
	if _, _, err := svc.Verify(ctx, "a@b.co", mailer.codes["a@b.co"], "Bob"); !errors.Is(err, ErrFull) {
		t.Fatalf("code from before the cap filled: want ErrFull, got %v", err)
	}

	if err := svc.RequestCode(ctx, "eve@example.com", ""); !errors.Is(err, ErrFull) {
		t.Fatalf("new email at the cap: want ErrFull, got %v", err)
	}
	if _, sent := mailer.codes["eve@example.com"]; sent {
		t.Fatal("a code was sent past the cap")
	}

	// Existing accounts still sign in.
	if err := svc.RequestCode(ctx, "ada@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Verify(ctx, "ada@example.com", mailer.codes["ada@example.com"], ""); err != nil {
		t.Fatal(err)
	}

	// Raising the cap lets the next person in.
	if err := svc.store.SetUserCap(ctx, 3); err != nil {
		t.Fatal(err)
	}
	signUp(t, svc, mailer, "eve@example.com", "Eve")
}

func TestDeleteAccountRemovesEverything(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)
	token := signUp(t, svc, mailer, "ada@example.com", "Ada")
	signUp(t, svc, mailer, "a@b.co", "Bob")
	ada, err := svc.store.GetUserByEmail(ctx, "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := svc.store.GetUserByEmail(ctx, "a@b.co")
	if err != nil {
		t.Fatal(err)
	}
	// A code requested and never used.
	if err := svc.RequestCode(ctx, "ada@example.com", ""); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteAccount(ctx, ada.ID); err != nil {
		t.Fatal(err)
	}

	conn := svc.store.db
	for table, where := range map[string]string{
		"users":       "id = ?1",
		"sessions":    "user_id = ?1",
		"login_codes": "email = ?2",
	} {
		var n int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE `+where, ada.ID, "ada@example.com").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if _, err := svc.Authenticate(ctx, token); err == nil {
		t.Error("the deleted account's session still works")
	}
	// Nobody else is touched.
	if _, err := svc.GetUser(ctx, bob.ID); err != nil {
		t.Errorf("other user: %v", err)
	}
	if err := svc.DeleteAccount(ctx, ada.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: want ErrNotFound, got %v", err)
	}
}

func TestInviteCodeGatesOnlyNewAccounts(t *testing.T) {
	ctx := context.Background()
	conn := testdb.Open(t)
	mailer := &fakeMailer{codes: map[string]string{}}
	svc := NewService(NewStore(conn), mailer)

	// Before there's a code, anyone can sign up; make one account then.
	if err := svc.RequestCode(ctx, "ada@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Verify(ctx, "ada@example.com", mailer.codes["ada@example.com"], "Ada"); err != nil {
		t.Fatal(err)
	}

	svc.RequireInviteCode("Summer-Plans")
	if err := svc.RequestCode(ctx, "bo@example.com", ""); !errors.Is(err, ErrNeedsInvite) {
		t.Fatalf("no code: %v", err)
	}
	if err := svc.RequestCode(ctx, "bo@example.com", "winter"); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, sent := mailer.codes["bo@example.com"]; sent {
		t.Fatal("a code was emailed without the invite code")
	}
	// Case, spaces and dashes don't matter.
	if err := svc.RequestCode(ctx, "bo@example.com", " summer plans "); err != nil {
		t.Fatalf("right code: %v", err)
	}
	// Existing accounts never need it.
	if err := svc.RequestCode(ctx, "ada@example.com", ""); err != nil {
		t.Fatalf("existing account: %v", err)
	}
	// Nor does someone a calendar member invited.
	if _, err := conn.ExecContext(ctx, `INSERT INTO calendars (id, kind, name, color) VALUES ('c1', 'general', 'Home', '#336699')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO calendar_invites (id, calendar_id, email) VALUES ('i1', 'c1', 'cy@example.com')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(ctx, "cy@example.com", ""); err != nil {
		t.Fatalf("invited to a calendar: %v", err)
	}
}

func TestCodeIsClaimedOnce(t *testing.T) {
	ctx := context.Background()
	svc, mailer, _ := newTestService(t)
	if err := svc.RequestCode(ctx, "a@b.co", ""); err != nil {
		t.Fatal(err)
	}
	lc, err := svc.store.LatestLoginCode(ctx, "a@b.co")
	if err != nil {
		t.Fatal(err)
	}
	// Someone else claims it between this sign-in's check and its claim.
	if ok, err := svc.store.ClaimLoginCode(ctx, lc.ID, time.Now()); err != nil || !ok {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if ok, _ := svc.store.ClaimLoginCode(ctx, lc.ID, time.Now()); ok {
		t.Fatal("a used code was claimed again")
	}
	if _, _, err := svc.Verify(ctx, "a@b.co", mailer.codes["a@b.co"], "A"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("verify after claim: %v", err)
	}
}
