package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	_ "time/tzdata" // zones for events and imports, whatever the host has

	"github.com/canavan-a/super-calendar/server/internal/auth"
	"github.com/canavan-a/super-calendar/server/internal/calendars"
	"github.com/canavan-a/super-calendar/server/internal/config"
	"github.com/canavan-a/super-calendar/server/internal/db"
	"github.com/canavan-a/super-calendar/server/internal/httpapi"
	"github.com/canavan-a/super-calendar/server/internal/live"
	"github.com/canavan-a/super-calendar/server/internal/mail"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	conn, err := db.Connect(ctx, cfg.DatabasePath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	if err := db.Migrate(ctx, conn); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	mailer := mail.NewSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPEmail, cfg.SMTPAppPassword, cfg.Dev)
	if (cfg.SMTPEmail == "" || cfg.SMTPAppPassword == "") && !cfg.Dev {
		log.Printf("warning: SMTP isn't configured, so nobody can sign in")
	}

	authService := auth.NewService(auth.NewStore(conn), mailer)
	authService.RequireInviteCode(cfg.InviteCode)
	if cfg.InviteCode == "" {
		log.Printf("SUPERCALENDAR_INVITE_CODE isn't set: anyone with an email address can sign up (up to the account cap)")
	} else {
		log.Printf("new accounts need the invite code (SUPERCALENDAR_INVITE_CODE)")
	}
	calendarService := calendars.NewService(conn, mailer)
	hub := live.NewHub()
	calendarService.UseLive(hub)
	go authService.RunJanitor(ctx)
	go calendarService.RunFeedSync(ctx) // subscribed calendars' links

	router := httpapi.NewRouter(httpapi.Deps{
		Config:       cfg,
		AuthService:  authService,
		AuthHandlers: auth.NewHandlers(authService),
		Calendars:    calendars.NewHandlers(calendarService),
		Live:         hub,
	})

	log.Printf("supercalendar listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
