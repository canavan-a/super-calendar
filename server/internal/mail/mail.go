package mail

import (
	"fmt"
	"html"
	"log"

	"gopkg.in/gomail.v2"
)

// Sender sends transactional email over SMTP (Gmail by default) using an
// app password.
type Sender struct {
	host        string
	port        int
	email       string
	appPassword string
	// logCodes prints codes to the server log instead of failing when SMTP
	// isn't configured, so local dev works without a mail account.
	logCodes bool
}

func NewSender(host string, port int, email, appPassword string, logCodes bool) *Sender {
	return &Sender{host: host, port: port, email: email, appPassword: appPassword, logCodes: logCodes}
}

func (s *Sender) SendLoginCode(to, code string) error {
	if s.email == "" || s.appPassword == "" {
		if s.logCodes {
			log.Printf("login code for %s: %s (SMTP not configured)", to, code)
			return nil
		}
		return fmt.Errorf("smtp not configured")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", m.FormatAddress(s.email, "Super Calendar"))
	m.SetHeader("To", to)
	m.SetHeader("Subject", fmt.Sprintf("%s is your Super Calendar code", code))
	m.SetBody("text/html", fmt.Sprintf(`
        <p>Your Super Calendar sign-in code is:</p>
        <h2 style="letter-spacing: 4px;">%s</h2>
        <p>It's valid for 10 minutes. If you didn't ask for it, you can ignore this email.</p>
    `, code))

	d := gomail.NewDialer(s.host, s.port, s.email, s.appPassword)
	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

var kindNames = map[string]string{"general": "calendar", "training": "training calendar", "cycle": "cycle calendar", "subscribed": "subscribed calendar"}

// SendInvite tells someone they've been invited to a calendar. It names the
// calendar but carries none of its data.
func (s *Sender) SendInvite(to, inviterName, calendarName, kind string) error {
	if s.email == "" || s.appPassword == "" {
		if s.logCodes {
			log.Printf("invite for %s to %q from %s (SMTP not configured)", to, calendarName, inviterName)
			return nil
		}
		return fmt.Errorf("smtp not configured")
	}
	what := kindNames[kind]
	if what == "" {
		what = "calendar"
	}
	m := gomail.NewMessage()
	m.SetHeader("From", m.FormatAddress(s.email, "Super Calendar"))
	m.SetHeader("To", to)
	m.SetHeader("Subject", fmt.Sprintf("%s invited you to %s on Super Calendar", inviterName, calendarName))
	m.SetBody("text/html", fmt.Sprintf(`
        <p><b>%s</b> invited you to the %s <b>%s</b> on Super Calendar.</p>
        <p>Get the app from <a href="%s">the latest release</a>, sign in with this email address,
        and accept the invite in your Inbox.</p>
    `, html.EscapeString(inviterName), what, html.EscapeString(calendarName), releasesURL))

	d := gomail.NewDialer(s.host, s.port, s.email, s.appPassword)
	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

const releasesURL = "https://github.com/canavan-a/super-calendar/releases/latest"
