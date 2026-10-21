package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"
)

// fakeSMTP accepts one message, returns what the client sent in DATA.
func fakeSMTP(t *testing.T, rejectRcpt bool) (port string, got chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { io.WriteString(c, s+"\r\n") }
		w("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250 fake")
			case strings.HasPrefix(cmd, "MAIL"):
				w("250 ok")
			case strings.HasPrefix(cmd, "RCPT"):
				if rejectRcpt {
					w("550 no such user")
				} else {
					w("250 ok")
				}
			case cmd == "DATA":
				w("354 go")
				var b strings.Builder
				for {
					l, _ := r.ReadString('\n')
					if l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				got <- b.String()
				w("250 queued")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, got
}

func TestSMTPDeliversMultipart(t *testing.T) {
	port, got := fakeSMTP(t, false)
	s := SMTP{Host: "127.0.0.1", Port: port, Timeout: 3 * time.Second}
	err := s.Send(context.Background(), Message{
		From: "Arman Curren <hello@armancurren.com>", To: "buyer@example.com", ReplyTo: "hello@armancurren.com",
		Subject: "Your order #1 — shipped", HTML: `<p>Hi <a href="https://x.test/t">track</a></p>`,
		Text: "Hi track (https://x.test/t)", MessageID: "mf-a-1-3@armancurren.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := <-got
	msg, err := stdmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("not a valid RFC5322 message: %v\n%s", err, raw)
	}
	if msg.Header.Get("Message-ID") != "<mf-a-1-3@armancurren.com>" || !strings.HasPrefix(msg.Header.Get("Content-Type"), "multipart/alternative") {
		t.Fatalf("bad headers: %v", msg.Header)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != "Your order #1 — shipped" {
		t.Fatalf("subject=%q", subj)
	}
}

func TestSMTPRejectedRecipientIsSafeToRetry(t *testing.T) {
	port, _ := fakeSMTP(t, true)
	s := SMTP{Host: "127.0.0.1", Port: port, Timeout: 3 * time.Second}
	err := s.Send(context.Background(), Message{From: "a@b.co", To: "c@d.co", Subject: "x", HTML: "x", Text: "x", MessageID: "i@b.co"})
	if !errors.Is(err, ErrNotSent) {
		t.Fatalf("pre-DATA rejection must wrap ErrNotSent: %v", err)
	}
}

func TestTextFromHTML(t *testing.T) {
	got := TextFromHTML(`<style>p{}</style><h1>Hi</h1><p>Go <a href="https://x.test">here</a></p><p>Bye &amp; thanks</p>`)
	want := "Hi\nGo here (https://x.test)\nBye & thanks\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
