package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const (
	userIDContextKey contextKey = "userID"
	tokenContextKey  contextKey = "token"
)

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		sess, err := s.session(r.Context(), token)
		if err != nil {
			if errors.Is(err, ErrInvalidSession) {
				writeError(w, http.StatusUnauthorized, "signed out")
				return
			}
			writeError(w, http.StatusInternalServerError, "could not check session")
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey, sess.UserID)
		ctx = context.WithValue(ctx, tokenContextKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserIDFromContext returns the authenticated user's ID, set by RequireAuth.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDContextKey).(string)
	return id, ok
}

func tokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(tokenContextKey).(string)
	return t
}
