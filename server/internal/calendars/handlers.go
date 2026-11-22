package calendars

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/canavan-a/super-calendar/server/internal/auth"
	"github.com/canavan-a/super-calendar/server/internal/httpx"
	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/go-chi/chi/v5"
)

type Handlers struct{ s *Service }

func NewHandlers(s *Service) *Handlers { return &Handlers{s: s} }

type ctxKey struct{}

func userID(r *http.Request) string {
	id, _ := auth.UserIDFromContext(r.Context())
	return id
}

func calendarFrom(r *http.Request) *Calendar {
	c, _ := r.Context().Value(ctxKey{}).(*Calendar)
	return c
}

// fail answers err: a message for bad input, 404 for anything not found (or
// not yours), 500 otherwise.
func fail(w http.ResponseWriter, err error) {
	var inv *InvalidError
	var conflict *ConflictError
	switch {
	case errors.As(err, &conflict):
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "changed by someone else", "conflict": true, "by": conflict.By, "current": conflict.Current,
		})
	case errors.As(err, &inv):
		httpx.WriteError(w, http.StatusBadRequest, inv.Msg)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrAlreadyMember):
		httpx.WriteError(w, http.StatusConflict, "they're already in this calendar")
	default:
		log.Printf("calendars: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "something went wrong")
	}
}

// RequireMember loads {calendarID} for the signed-in user. Anyone who isn't a
// member gets the same 404 as for a calendar that doesn't exist.
func (h *Handlers) RequireMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := h.s.Get(r.Context(), chi.URLParam(r, "calendarID"), userID(r))
		if err != nil {
			fail(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, c)))
	})
}

// RequireKind only lets requests through for calendars of kind.
func RequireKind(kind string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calendarFrom(r).Kind != kind {
				httpx.WriteError(w, http.StatusNotFound, "not found")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (h *Handlers) Routes(r chi.Router) {
	r.Get("/calendars", h.list)
	r.Post("/calendars", h.create)
	r.Put("/calendars/order", h.reorder)
	r.Get("/invites", h.inbox)
	r.Post("/invites/{inviteID}/accept", h.respond(true))
	r.Post("/invites/{inviteID}/decline", h.respond(false))
	r.Get("/overview", h.overview)
	r.Get("/me/settings", h.getSettings)
	r.Put("/me/settings", h.putSettings)

	r.Route("/calendars/{calendarID}", func(r chi.Router) {
		r.Use(h.RequireMember)
		r.Use(h.Notify)
		r.Get("/", h.get)
		r.Patch("/", h.update)
		r.Patch("/me", h.updateMine)
		r.Delete("/me", h.leave)
		r.Get("/members", h.members)
		r.Get("/invites", h.sentInvites)
		r.Post("/invites", h.invite)
		r.Delete("/invites/{inviteID}", h.cancelInvite)

		r.Group(func(r chi.Router) {
			r.Use(RequireKind(General))
			r.Get("/events", h.listEvents)
			r.Post("/events", h.createEvent)
			r.Get("/events/{eventID}", h.getEvent)
			r.Patch("/events/{eventID}", h.updateEvent)
			r.Delete("/events/{eventID}", h.deleteEvent)
			r.Post("/import/preview", h.previewImport)
			r.Post("/import", h.importICS)
		})
		r.Group(func(r chi.Router) {
			r.Use(RequireKind(Training))
			r.Get("/activity-types", h.listActivityTypes)
			r.Post("/activity-types", h.createActivityType)
			r.Patch("/activity-types/{typeID}", h.updateActivityType)
			r.Get("/sessions", h.listSessions)
			r.Post("/sessions", h.createSession)
			r.Get("/sessions/{sessionID}", h.getSession)
			r.Patch("/sessions/{sessionID}", h.updateSession)
			r.Delete("/sessions/{sessionID}", h.deleteSession)
			r.Post("/sessions/copy", h.copySessions)
			r.Post("/sessions/restore", h.restoreSessions)
		})
		r.Group(func(r chi.Router) {
			r.Use(RequireKind(Cycle))
			r.Get("/cycle/days", h.listCycleDays)
			r.Put("/cycle/days/{date}", h.putCycleDay)
			r.Get("/cycle/summary", h.cycleSummary)
		})
	})
}

// --- Calendars, members, invites ---

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	cals, err := h.s.List(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"calendars": cals})
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	var req struct{ Kind, Name, Color string }
	if !httpx.Decode(w, r, &req) {
		return
	}
	c, err := h.s.Create(r.Context(), userID(r), req.Kind, req.Name, req.Color)
	if err != nil {
		fail(w, err)
		return
	}
	h.s.toSelf(r, "calendars")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"calendar": c})
}

func (h *Handlers) reorder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.s.Reorder(r.Context(), userID(r), req.IDs); err != nil {
		fail(w, err)
		return
	}
	h.s.toSelf(r, "calendars")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"calendar": calendarFrom(r)})
}

func (h *Handlers) refreshed(w http.ResponseWriter, r *http.Request) {
	c, err := h.s.Get(r.Context(), calendarFrom(r).ID, userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"calendar": c})
}

func (h *Handlers) update(w http.ResponseWriter, r *http.Request) {
	var p CalendarPatch
	if !httpx.Decode(w, r, &p) {
		return
	}
	if err := h.s.Update(r.Context(), calendarFrom(r), p); err != nil {
		fail(w, err)
		return
	}
	h.refreshed(w, r)
}

func (h *Handlers) updateMine(w http.ResponseWriter, r *http.Request) {
	var p MemberPatch
	if !httpx.Decode(w, r, &p) {
		return
	}
	if err := h.s.UpdateMine(r.Context(), calendarFrom(r), userID(r), p); err != nil {
		fail(w, err)
		return
	}
	h.refreshed(w, r)
}

func (h *Handlers) leave(w http.ResponseWriter, r *http.Request) {
	if err := h.s.Leave(r.Context(), calendarFrom(r).ID, userID(r)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) members(w http.ResponseWriter, r *http.Request) {
	m, err := h.s.Members(r.Context(), calendarFrom(r).ID)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"members": m})
}

func (h *Handlers) sentInvites(w http.ResponseWriter, r *http.Request) {
	inv, err := h.s.SentInvites(r.Context(), calendarFrom(r).ID)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invites": inv})
}

func (h *Handlers) invite(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string }
	if !httpx.Decode(w, r, &req) {
		return
	}
	inv, err := h.s.Invite(r.Context(), calendarFrom(r), userID(r), req.Email)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"invite": inv})
}

func (h *Handlers) cancelInvite(w http.ResponseWriter, r *http.Request) {
	if err := h.s.CancelInvite(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "inviteID")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) inbox(w http.ResponseWriter, r *http.Request) {
	inv, err := h.s.Inbox(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invites": inv})
}

func (h *Handlers) respond(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := h.s.Respond(r.Context(), userID(r), chi.URLParam(r, "inviteID"), accept)
		if err != nil {
			fail(w, err)
			return
		}
		h.s.toSelf(r, "inbox")
		if c != nil {
			h.s.toSelf(r, "calendars")
			me := userID(r)
			h.s.send(h.s.memberIDs(r.Context(), c.ID), live.Message{Type: "changed", CalendarID: c.ID, Area: "members",
				By: me, ByName: h.s.displayName(r.Context(), me), Origin: origin(r)})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"calendar": c})
	}
}

func (h *Handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.UserSettings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"settings": v})
}

func (h *Handlers) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings json.RawMessage `json:"settings"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.s.SetUserSettings(r.Context(), userID(r), req.Settings); err != nil {
		fail(w, err)
		return
	}
	h.s.toSelf(r, "calendars")
	w.WriteHeader(http.StatusNoContent)
}

func rangeFrom(w http.ResponseWriter, r *http.Request) (Range, bool) {
	q := r.URL.Query()
	rg, err := ParseRange(q.Get("from"), q.Get("to"), q.Get("tz"))
	if err != nil {
		fail(w, err)
		return Range{}, false
	}
	return rg, true
}

func (h *Handlers) overview(w http.ResponseWriter, r *http.Request) {
	rg, ok := rangeFrom(w, r)
	if !ok {
		return
	}
	items, err := h.s.Overview(r.Context(), userID(r), rg)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- General: events and import ---

func (h *Handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	rg, ok := rangeFrom(w, r)
	if !ok {
		return
	}
	occ, err := h.s.Occurrences(r.Context(), []string{calendarFrom(r).ID}, rg)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"occurrences": occ})
}

func (h *Handlers) getEvent(w http.ResponseWriter, r *http.Request) {
	e, err := h.s.GetEvent(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "eventID"))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": e})
}

func (h *Handlers) createEvent(w http.ResponseWriter, r *http.Request) {
	var in EventInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	e, err := h.s.CreateEvent(r.Context(), calendarFrom(r).ID, userID(r), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"event": e})
}

func (h *Handlers) updateEvent(w http.ResponseWriter, r *http.Request) {
	var in EventInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	e, err := h.s.UpdateEvent(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "eventID"), userID(r), r.URL.Query().Get("occurrence"), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": e})
}

func (h *Handlers) deleteEvent(w http.ResponseWriter, r *http.Request) {
	if err := h.s.DeleteEvent(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "eventID"), userID(r), r.URL.Query().Get("occurrence")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) previewImport(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	p, err := h.s.PreviewImport(r.Context(), calendarFrom(r).ID, req)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (h *Handlers) importICS(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	res, err := h.s.Import(r.Context(), calendarFrom(r).ID, userID(r), req)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// --- Training ---

func (h *Handlers) listActivityTypes(w http.ResponseWriter, r *http.Request) {
	t, err := h.s.ActivityTypes(r.Context(), calendarFrom(r).ID)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"activityTypes": t})
}

func (h *Handlers) createActivityType(w http.ResponseWriter, r *http.Request) {
	var in ActivityTypeInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	t, err := h.s.CreateActivityType(r.Context(), calendarFrom(r).ID, in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"activityType": t})
}

func (h *Handlers) updateActivityType(w http.ResponseWriter, r *http.Request) {
	var in ActivityTypeInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	t, err := h.s.UpdateActivityType(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "typeID"), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"activityType": t})
}

func (h *Handlers) listSessions(w http.ResponseWriter, r *http.Request) {
	rg, ok := rangeFrom(w, r)
	if !ok {
		return
	}
	t, err := h.s.Sessions(r.Context(), []string{calendarFrom(r).ID}, rg.FromDate, rg.ToDate)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": t})
}

func (h *Handlers) getSession(w http.ResponseWriter, r *http.Request) {
	t, err := h.s.GetSession(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "sessionID"))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"session": t})
}

func (h *Handlers) createSession(w http.ResponseWriter, r *http.Request) {
	var in SessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	t, err := h.s.CreateSession(r.Context(), calendarFrom(r).ID, userID(r), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"session": t})
}

func (h *Handlers) updateSession(w http.ResponseWriter, r *http.Request) {
	var in SessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	t, err := h.s.UpdateSession(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "sessionID"), userID(r), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"session": t})
}

func (h *Handlers) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := h.s.DeleteSession(r.Context(), calendarFrom(r).ID, chi.URLParam(r, "sessionID")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) copySessions(w http.ResponseWriter, r *http.Request) {
	var req CopyRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	res, err := h.s.Copy(r.Context(), calendarFrom(r).ID, userID(r), req)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (h *Handlers) restoreSessions(w http.ResponseWriter, r *http.Request) {
	var req RestoreRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.s.Restore(r.Context(), calendarFrom(r).ID, userID(r), req); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Cycle ---

func (h *Handlers) listCycleDays(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d, err := h.s.CycleDays(r.Context(), calendarFrom(r).ID, q.Get("from"), q.Get("to"))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"days": d})
}

func (h *Handlers) putCycleDay(w http.ResponseWriter, r *http.Request) {
	var req struct{ Flow, Notes string }
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.s.PutCycleDay(r.Context(), calendarFrom(r).ID, userID(r), chi.URLParam(r, "date"), req.Flow, req.Notes); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) cycleSummary(w http.ResponseWriter, r *http.Request) {
	rg, err := ParseRange("2000-01-01", "2000-01-02", r.URL.Query().Get("tz"))
	if err != nil {
		fail(w, err)
		return
	}
	sum, err := h.s.CycleSummary(r.Context(), calendarFrom(r).ID, rg.Loc)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sum)
}
