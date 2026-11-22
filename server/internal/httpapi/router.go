package httpapi

import (
	"net/http"
	"time"

	"github.com/canavan-a/super-calendar/server/internal/auth"
	"github.com/canavan-a/super-calendar/server/internal/calendars"
	"github.com/canavan-a/super-calendar/server/internal/config"
	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/canavan-a/super-calendar/server/internal/ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Deps struct {
	Config       *config.Config
	AuthService  *auth.Service
	AuthHandlers *auth.Handlers
	Calendars    *calendars.Handlers
	// Live is where change notifications go out (GET /live).
	Live *live.Hub
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	byIP := ratelimit.IPKeyer(d.Config.TrustProxy)
	limit := func(n int, per time.Duration) func(http.Handler) http.Handler {
		return ratelimit.Middleware(ratelimit.New(n, per), byIP)
	}
	// Behind RequireAuth: per account, so a signed-in user can't get around
	// it by changing IPs, and people behind one IP don't share a limit.
	byUser := func(r *http.Request) string {
		id, _ := auth.UserIDFromContext(r.Context())
		return id
	}
	limitUser := func(n int, per time.Duration) func(http.Handler) http.Handler {
		return ratelimit.Middleware(ratelimit.New(n, per), byUser)
	}

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.With(limit(60, time.Minute)).Get("/config", d.AuthHandlers.Config)

	r.Route("/auth", func(r chi.Router) {
		r.With(limit(10, 10*time.Minute)).Post("/code", d.AuthHandlers.Code)
		r.With(limit(30, 10*time.Minute)).Post("/verify", d.AuthHandlers.Verify)
	})

	r.Group(func(r chi.Router) {
		// Ceilings on everything, per IP before checking the token (so junk
		// tokens can't hammer the database) and per account after.
		r.Use(limit(600, time.Minute))
		r.Use(d.AuthService.RequireAuth)
		r.Use(limitUser(300, time.Minute))

		r.Get("/me", d.AuthHandlers.Me)
		r.Delete("/me", d.AuthHandlers.DeleteMe)
		r.Post("/auth/logout", d.AuthHandlers.Logout)

		// The live socket: what changed, so open apps refetch (internal/live).
		r.Get("/live", func(w http.ResponseWriter, r *http.Request) {
			id, _ := auth.UserIDFromContext(r.Context())
			d.Live.Serve(w, r, id)
		})
		d.Calendars.Routes(r)
	})

	return r
}
