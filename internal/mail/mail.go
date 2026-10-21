// Package mail renders step templates and delivers them.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"time"
)

type Message struct {
	From, To, ReplyTo string
	Subject           string
	HTML, Text        string
	MessageID         string // without angle brackets
}

// ErrNotSent wraps failures that happened before the server accepted the DATA
// phase, so retrying cannot produce a duplicate email.
var ErrNotSent = errors.New("not sent")

type Sender interface {
	Send(ctx context.Context, m Message) error
	Name() string
}

// ---- log sender (dry run) ----

type LogSender struct{ Log *slog.Logger }

func (LogSender) Name() string { return "log (dry-run, nothing leaves the machine)" }
func (l LogSender) Send(_ context.Context, m Message) error {
	l.Log.Info("DRY-RUN email", "to", m.To, "from", m.From, "subject", m.Subject, "message_id", m.MessageID, "bytes", len(m.HTML))
	return nil
}

// ---- SMTP sender ----

type SMTP struct {
	Host, Port, User, Pass string
	Timeout                time.Duration
}

func (s SMTP) Name() string { return "smtp " + s.Host + ":" + s.Port }

func (s SMTP) Send(ctx context.Context, m Message) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	envFrom, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("%w: from: %v", ErrNotSent, err)
	}
	envTo, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("%w: to: %v", ErrNotSent, err)
	}
	d := net.Dialer{Timeout: timeout}
	addr := net.JoinHostPort(s.Host, s.Port)
	var conn net.Conn
	if s.Port == "465" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: s.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("%w: dial: %v", ErrNotSent, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * timeout))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("%w: handshake: %v", ErrNotSent, err)
	}
	defer c.Close()
	if s.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return fmt.Errorf("%w: starttls: %v", ErrNotSent, err)
			}
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Pass, s.Host)); err != nil {
			return fmt.Errorf("%w: auth: %v", ErrNotSent, err)
		}
	}
	if err := c.Mail(envFrom.Address); err != nil {
		return fmt.Errorf("%w: MAIL FROM: %v", ErrNotSent, err)
	}
	if err := c.Rcpt(envTo.Address); err != nil {
		return fmt.Errorf("%w: RCPT TO: %v", ErrNotSent, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("%w: DATA: %v", ErrNotSent, err)
	}
	if _, err := w.Write(Build(m)); err != nil {
		return fmt.Errorf("smtp write: %w", err) // ambiguous: may have been delivered
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp commit: %w", err)
	}
	return c.Quit()
}

// Build assembles a multipart/alternative RFC 5322 message.
func Build(m Message) []byte {
	var b bytes.Buffer
	boundary := "mf-" + strings.ReplaceAll(m.MessageID, "@", "-")
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", m.From)
	h("To", m.To)
	if m.ReplyTo != "" {
		h("Reply-To", m.ReplyTo)
	}
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", time.Now().Format(time.RFC1123Z))
	if m.MessageID != "" {
		h("Message-ID", "<"+m.MessageID+">")
	}
	h("MIME-Version", "1.0")
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	part := func(ctype, body string) {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", boundary, ctype)
		enc := base64.StdEncoding.EncodeToString([]byte(body))
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	part("text/plain", m.Text)
	part("text/html", m.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}

var (
	reLink  = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	reBlock = regexp.MustCompile(`(?i)</(p|div|h[1-6]|tr|li|table)>|<br\s*/?>`)
	reTag   = regexp.MustCompile(`(?s)<[^>]+>`)
	reStyle = regexp.MustCompile(`(?is)<(style|head)[^>]*>.*?</(style|head)>`)
	reBlank = regexp.MustCompile(`\n{3,}`)
)

// TextFromHTML makes a readable plain-text alternative from the HTML body.
func TextFromHTML(s string) string {
	s = reStyle.ReplaceAllString(s, "")
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := reLink.FindStringSubmatch(m)
		label := strings.TrimSpace(reTag.ReplaceAllString(sm[2], ""))
		if label == "" || label == sm[1] {
			return sm[1]
		}
		return label + " (" + sm[1] + ")"
	})
	s = reBlock.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")) + "\n"
}
