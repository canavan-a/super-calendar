package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/httpx"
	"github.com/canavan-a/super-calendar/server/internal/ratelimit"
)

type Handlers struct {
	service *Service
	// Per email, however many IPs ask: one code a minute and ten a day, and
	// ten guesses an hour across all its codes (each code also allows only
	// five), so a code can't be guessed by asking for new ones.
	codesPerEmail      *ratelimit.Limiter
	codesPerEmailDaily *ratelimit.Limiter
	guessesPerEmail    *ratelimit.Limiter
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{
		service:            service,
		codesPerEmail:      ratelimit.New(1, time.Minute),
		codesPerEmailDaily: ratelimit.New(10, 24*time.Hour),
		guessesPerEmail:    ratelimit.New(10, time.Hour),
	}
}

// The shared helpers (internal/httpx), under the names this package uses.
var (
	writeJSON  = httpx.WriteJSON
	writeError = httpx.WriteError
)

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

func toUserResponse(u *User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
}

const fullMessage = "Super Calendar is full right now, ask whoever runs it to make room"

type codeRequest struct {
	Email string `json:"email"`
	// Only needed for a new account, when the server has an invite code.
	InviteCode string `json:"inviteCode"`
}

// Code emails a sign-in code. The response is the same whether or not the
// email has an account.
func (h *Handlers) Code(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	email := NormalizeEmail(req.Email)
	// The invite code first, so a missing or mistyped one doesn't use up the
	// email's one code a minute.
	if err := h.service.Admit(r.Context(), email, req.InviteCode); err != nil {
		h.codeError(w, err)
		return
	}
	if !h.codesPerEmail.Allow(email) {
		writeError(w, http.StatusTooManyRequests, "wait a minute before asking for another code")
		return
	}
	if !h.codesPerEmailDaily.Allow(email) {
		writeError(w, http.StatusTooManyRequests, "too many codes today, try again tomorrow")
		return
	}
	if err := h.service.RequestCode(r.Context(), req.Email, req.InviteCode); err != nil {
		h.codeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) codeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "enter a valid email")
	case errors.Is(err, ErrFull):
		writeError(w, http.StatusForbidden, fullMessage)
	case errors.Is(err, ErrNeedsInvite):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "new accounts need an invite code", "needsInvite": true})
	case errors.Is(err, ErrBadInvite):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "that invite code isn't right", "needsInvite": true})
	case errors.Is(err, ErrTooManyBadInvites):
		writeError(w, http.StatusTooManyRequests, "too many wrong invite codes, try again in a while")
	case errors.Is(err, ErrTooManyCodes):
		writeError(w, http.StatusTooManyRequests, "too many sign-ins right now, try again in a while")
	default:
		writeError(w, http.StatusInternalServerError, "could not send code")
	}
}

type verifyRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	DisplayName string `json:"displayName"`
}

type verifyResponse struct {
	User  userResponse `json:"user"`
	Token string       `json:"token"`
}

func (h *Handlers) Verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !h.guessesPerEmail.Allow(NormalizeEmail(req.Email)) {
		writeError(w, http.StatusTooManyRequests, "too many tries, wait a while and ask for a new code")
		return
	}
	user, token, err := h.service.Verify(r.Context(), req.Email, req.Code, req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCode):
			writeError(w, http.StatusUnauthorized, "that code is wrong or expired")
		case errors.Is(err, ErrFull):
			writeError(w, http.StatusForbidden, fullMessage)
		case errors.Is(err, ErrNeedsName):
			writeJSON(w, http.StatusConflict, map[string]any{"error": "new account needs a name", "needsName": true})
		default:
			writeError(w, http.StatusInternalServerError, "could not sign in")
		}
		return
	}
	writeJSON(w, http.StatusOK, verifyResponse{User: toUserResponse(user), Token: token})
}

type meResponse struct {
	User userResponse `json:"user"`
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserIDFromContext(r.Context())
	user, err := h.service.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "signed out")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load account")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: toUserResponse(user)})
}

// Logout ends the session the request was made with.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Logout(r.Context(), tokenFromContext(r.Context())); err != nil {
		writeError(w, http.StatusInternalServerError, "could not sign out")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteMe deletes the signed-in account and everything tied to it.
func (h *Handlers) DeleteMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserIDFromContext(r.Context())
	if err := h.service.DeleteAccount(r.Context(), userID); err != nil && !errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "could not delete account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Config tells the app, before sign-in, what the server asks of new
// accounts, so it can show the invite code field up front.
func (h *Handlers) Config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"app": "supercalendar", "inviteCode": h.service.InviteCodeRequired()})
}
