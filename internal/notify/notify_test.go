package notify

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/larryjoelane/healbot/internal/model"
)

func TestTeamsWebhook(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		got = buf.String()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	err := Webhook{HookName: "teams", URL: srv.URL, Format: "teams", HTTP: srv.Client()}.Send(context.Background(), Message{Subject: "sev2 boom", Body: "details"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "MessageCard") || !strings.Contains(got, "sev2 boom") {
		t.Fatal(got)
	}
}

func TestSMTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		_, _ = rw.WriteString("220 healbot.test ESMTP\r\n")
		_ = rw.Flush()
		for {
			line, err := rw.ReadString('\n')
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				_, _ = rw.WriteString("250 hello\r\n")
			case strings.HasPrefix(upper, "MAIL"), strings.HasPrefix(upper, "RCPT"):
				_, _ = rw.WriteString("250 ok\r\n")
			case strings.HasPrefix(upper, "DATA"):
				_, _ = rw.WriteString("354 go\r\n")
				_ = rw.Flush()
				for {
					l, err := rw.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
				}
				_, _ = rw.WriteString("250 queued\r\n")
			case strings.HasPrefix(upper, "QUIT"):
				_, _ = rw.WriteString("221 bye\r\n")
				_ = rw.Flush()
				return
			default:
				_, _ = rw.WriteString("250 ok\r\n")
			}
			_ = rw.Flush()
		}
	}()
	err = SMTPMailer{Addr: ln.Addr().String(), From: "healbot@example.com", To: []string{"oncall@example.com"}}.Send(context.Background(), Message{Subject: "sev2 api", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestRender(t *testing.T) {
	msg := FromIncident(model.Incident{ID: "inc_1", Title: "NullReferenceException in OrderService.Place", Severity: model.Sev2, Status: model.StatusPROpened, TierReached: model.TierCodeFix}, "pr.opened")
	if !strings.Contains(msg.Subject, "sev2") || !strings.Contains(msg.Body, "inc_1") {
		t.Fatal(msg)
	}
}
