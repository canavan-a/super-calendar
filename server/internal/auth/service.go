package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/ratelimit"
	"github.com/google/uuid"
)

var (
	ErrInvalidEmail   = errors.New("invalid email")
	ErrInvalidCode    = errors.New("invalid or expired code")
	ErrNeedsName      = errors.New("display name required for a new account")
	ErrInvalidSession = errors.New("invalid or expired session")
	ErrFull           = errors.New("the user cap is reached")
	ErrTooManyCodes   = errors.New("too many sign-in codes sent")
	// ErrNeedsInvite: a new account needs the server's invite code.
	ErrNeedsInvite       = errors.New("new accounts need the invite code")
	ErrBadInvite         = errors.New("wrong invite code")
	ErrTooManyBadInvites = errors.New("too many wrong invite codes")
)

const (
	codeTTL         = 10 * time.Minute
	maxCodeAttempts = 5
	// A session is only ever signed out after this long without being used.
	sessionIdleTTL = 90 * 24 * time.Hour
	// last_used_at/expires_at are rewritten at most this often per session,
	// so ordinary requests don't each cost a database write.
	sessionTouchEvery = time.Hour
	// The most sign-in codes sent in an hour, to everyone together: a cap on
	// what the server can make the mail account send, well under Gmail's
	// daily limit.
	codesPerHour = 100
)

// Mailer sends the sign-in code out of band; satisfied by mail.Sender.
type Mailer interface {
	SendLoginCode(to, code string) error
}

type Service struct {
	store  *Store
	mailer Mailer
	now    func() time.Time
	sends  *ratelimit.Limiter
	// inviteCode, when set, is what a new account must give (see admit).
	inviteCode string
	// Wrong invite codes from everyone together, so it can't be guessed.
	badInvites *ratelimit.Limiter
}

func NewService(store *Store, mailer Mailer) *Service {
	return &Service{store: store, mailer: mailer, now: time.Now, sends: ratelimit.New(codesPerHour, time.Hour), badInvites: ratelimit.New(30, time.Hour)}
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// RequestCode emails a fresh 6-digit code to email. Anyone with an account
// gets one; anyone else only while there are fewer accounts than the user cap
// (ErrFull otherwise, and nothing is sent), since the same code signs up.
func (s *Service) RequestCode(ctx context.Context, email, invite string) error {
	email = NormalizeEmail(email)
	if !strings.Contains(email, "@") || len(email) > 254 {
		return ErrInvalidEmail
	}
	if _, err := s.store.GetUserByEmail(ctx, email); errors.Is(err, ErrNotFound) {
		if err := s.admit(ctx, email, invite); err != nil {
			return err
		}
		if err := s.checkRoom(ctx, email); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !s.sends.Allow("all") {
		return ErrTooManyCodes
	}

	code, err := generateCode()
	if err != nil {
		return err
	}
	if err := s.store.CreateLoginCode(ctx, uuid.NewString(), email, HashToken(code), s.now().Add(codeTTL)); err != nil {
		return err
	}
	return s.mailer.SendLoginCode(email, code)
}

// Verify checks code against the latest one sent to email and, if it
// matches, signs in (creating the account when there isn't one yet) and
// returns a new session token. A new account needs displayName; without one
// it returns ErrNeedsName and leaves the code usable for the retry.
func (s *Service) Verify(ctx context.Context, email, code, displayName string) (*User, string, error) {
	email = NormalizeEmail(email)
	displayName = strings.TrimSpace(displayName)

	lc, err := s.store.LatestLoginCode(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", ErrInvalidCode
		}
		return nil, "", err
	}
	if lc.UsedAt != nil || s.now().After(lc.ExpiresAt) || lc.Attempts >= maxCodeAttempts {
		return nil, "", ErrInvalidCode
	}
	if subtle.ConstantTimeCompare([]byte(lc.CodeHash), []byte(HashToken(strings.TrimSpace(code)))) != 1 {
		if err := s.store.IncrementLoginCodeAttempts(ctx, lc.ID); err != nil {
			return nil, "", err
		}
		return nil, "", ErrInvalidCode
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	isNew := errors.Is(err, ErrNotFound)
	if err != nil && !isNew {
		return nil, "", err
	}
	if isNew {
		// The last place may have gone since the code was sent.
		if err := s.checkRoom(ctx, email); err != nil {
			return nil, "", err
		}
		if displayName == "" {
			return nil, "", ErrNeedsName
		}
	}

	// Claim the code before making anything, so it signs in only once.
	if claimed, err := s.store.ClaimLoginCode(ctx, lc.ID, s.now()); err != nil {
		return nil, "", err
	} else if !claimed {
		return nil, "", ErrInvalidCode
	}
	if isNew {
		if user, err = s.createUser(ctx, email, displayName); err != nil {
			return nil, "", err
		}
	}
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// checkRoom returns ErrFull if a new account for email can't be made because
// the user cap is reached. Someone with a pending calendar invite is let in
// anyway: a member vouched for them.
func (s *Service) checkRoom(ctx context.Context, email string) error {
	if invited, err := s.store.HasPendingInvite(ctx, email); err != nil || invited {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	limit, err := s.store.UserCap(ctx)
	if err != nil {
		return err
	}
	if n >= limit {
		return ErrFull
	}
	return nil
}

func (s *Service) createUser(ctx context.Context, email, displayName string) (*User, error) {
	id := uuid.NewString()
	if err := s.store.CreateUser(ctx, id, email, displayName); err != nil {
		// Two verifies for the same new email racing: the loser's insert
		// hits the UNIQUE constraint, and the winner's account is the one.
		if u, getErr := s.store.GetUserByEmail(ctx, email); getErr == nil {
			return u, nil
		}
		return nil, err
	}
	return s.store.GetUserByID(ctx, id)
}

func (s *Service) createSession(ctx context.Context, userID string) (string, error) {
	raw, hash, err := GenerateOpaqueToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	if err := s.store.CreateSession(ctx, uuid.NewString(), userID, hash, now, now.Add(sessionIdleTTL)); err != nil {
		return "", err
	}
	return raw, nil
}

// Authenticate resolves a session token to its user, sliding the session's
// expiry forward as it's used.
func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	sess, err := s.session(ctx, token)
	if err != nil {
		return "", err
	}
	return sess.UserID, nil
}

func (s *Service) session(ctx context.Context, token string) (*Session, error) {
	sess, err := s.store.GetSessionByHash(ctx, HashToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrInvalidSession
		}
		return nil, err
	}
	now := s.now()
	if now.After(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.ID)
		return nil, ErrInvalidSession
	}
	if now.Sub(sess.LastUsedAt) > sessionTouchEvery {
		if err := s.store.TouchSession(ctx, sess.ID, now, now.Add(sessionIdleTTL)); err != nil {
			return nil, err
		}
	}
	return sess, nil
}

// Logout ends the session for token. Unknown tokens are a no-op.
func (s *Service) Logout(ctx context.Context, token string) error {
	sess, err := s.store.GetSessionByHash(ctx, HashToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return s.store.DeleteSession(ctx, sess.ID)
}

func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	return s.store.GetUserByID(ctx, id)
}

// DeleteAccount deletes userID's account and all its data, signing out every
// device it was on.
func (s *Service) DeleteAccount(ctx context.Context, userID string) error {
	return s.store.DeleteUser(ctx, userID)
}

// RunJanitor deletes expired codes and sessions every hour until ctx ends.
func (s *Service) RunJanitor(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		now := s.now()
		if err := s.store.DeleteExpiredLoginCodes(ctx, now); err != nil {
			log.Printf("janitor: login codes: %v", err)
		}
		if err := s.store.DeleteExpiredSessions(ctx, now); err != nil {
			log.Printf("janitor: sessions: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RequireInviteCode makes new accounts give code before a sign-in code is
// sent to them ("" turns it off). Existing accounts never need it, and nor
// does anyone with a pending calendar invite: a member has vouched for them.
func (s *Service) RequireInviteCode(code string) { s.inviteCode = code }

// InviteCodeRequired reports whether new accounts need the invite code.
func (s *Service) InviteCodeRequired() bool { return s.inviteCode != "" }

// admit checks a new email's invite code, if the server has one.
func (s *Service) admit(ctx context.Context, email, invite string) error {
	if s.inviteCode == "" {
		return nil
	}
	if invited, err := s.store.HasPendingInvite(ctx, email); err != nil || invited {
		return err
	}
	if strings.TrimSpace(invite) == "" {
		return ErrNeedsInvite
	}
	if !s.badInvites.Peek("all") {
		return ErrTooManyBadInvites
	}
	// Hashed so the comparison doesn't depend on the code's length.
	got, want := sha256.Sum256([]byte(normalizeInvite(invite))), sha256.Sum256([]byte(normalizeInvite(s.inviteCode)))
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		s.badInvites.Allow("all")
		return ErrBadInvite
	}
	return nil
}

// normalizeInvite ignores case, spaces and dashes, so "summer plans" matches
// SUMMER-PLANS.
func normalizeInvite(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if r != ' ' && r != '-' && r != '\t' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Admit checks the invite code for email without sending anything, so the
// handler can turn away a missing or wrong code before rate limits count it.
func (s *Service) Admit(ctx context.Context, email, invite string) error {
	email = NormalizeEmail(email)
	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.admit(ctx, email, invite)
}
