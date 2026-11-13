// Package notify delivers incident messages. Every channel implements Delivery
// so the pipeline never imports a specific provider.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

// Message is the email-shaped payload every channel accepts.
type Message struct {
	IncidentID string
	Subject    string
	Body       string
	Severity   model.Severity
	Tier       model.Tier
}

// Delivery sends one message.
type Delivery interface {
	Name() string
	Send(ctx context.Context, msg Message) error
}

// FromIncident renders a message a human can read without the console.
func FromIncident(inc model.Incident, kind string) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nIncident: %s\nStatus: %s\nTier reached: %s\nResource: %s\nCount: %d\n",
		inc.Title, inc.ID, inc.Status, inc.TierReached, inc.ResourceID, inc.Count)
	if inc.Hypothesis != nil {
		fmt.Fprintf(&b, "\nHypothesis (%s, %.0f%%): %s\n", inc.Hypothesis.Provider, inc.Hypothesis.Confidence*100, inc.Hypothesis.Summary)
	}
	if len(inc.Actions) > 0 {
		last := inc.Actions[len(inc.Actions)-1]
		fmt.Fprintf(&b, "\nLast action: %s %s (%s)\n", last.Kind, last.Outcome, last.Detail)
	}
	if inc.PullRequest != nil {
		fmt.Fprintf(&b, "\nPull request (not merged): %s\n%s\n", inc.PullRequest.Title, inc.PullRequest.URL)
	}
	if inc.Issue != nil {
		fmt.Fprintf(&b, "\nIssue (%s, not a merge): %s\n%s\n", inc.Issue.Agent, inc.Issue.Title, inc.Issue.URL)
	}
	fmt.Fprintf(&b, "\nEvent: %s\n", kind)
	return Message{
		IncidentID: inc.ID,
		Subject:    fmt.Sprintf("[healbot] %s %s", inc.Severity, inc.Title),
		Body:       b.String(),
		Severity:   inc.Severity,
		Tier:       inc.TierReached,
	}
}

// Log writes the message to a logger function. Used for stdout delivery.
type Log struct {
	Printf func(string, ...any)
}

func (l Log) Name() string { return "stdout" }

func (l Log) Send(_ context.Context, msg Message) error {
	fn := l.Printf
	if fn == nil {
		fn = func(string, ...any) {}
	}
	fn("notify %s %s\n%s", msg.Severity, msg.Subject, msg.Body)
	return nil
}

// Webhook posts to Teams, Slack, or a generic JSON endpoint.
type Webhook struct {
	HookName string
	URL      string
	Format   string
	HTTP     *http.Client
}

func (w Webhook) Name() string {
	if w.HookName != "" {
		return w.HookName
	}
	return "webhook"
}

func (w Webhook) Send(ctx context.Context, msg Message) error {
	if w.URL == "" {
		return fmt.Errorf("webhook %s has no url", w.Name())
	}
	var payload any
	switch strings.ToLower(w.Format) {
	case "slack":
		payload = map[string]string{"text": msg.Subject + "\n" + msg.Body}
	case "teams":
		payload = map[string]string{
			"@type":      "MessageCard",
			"@context":   "http://schema.org/extensions",
			"themeColor": "E3A008",
			"summary":    msg.Subject,
			"title":      msg.Subject,
			"text":       msg.Body,
		}
	default:
		payload = msg
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 300 {
		return fmt.Errorf("webhook %s status %d", w.Name(), res.StatusCode)
	}
	return nil
}

// GitHubIssue opens a tracking issue. It is not a code change.
type GitHubIssue struct {
	BaseURL string
	Owner   string
	Repo    string
	Token   string
	HTTP    *http.Client
}

func (g GitHubIssue) Name() string { return "github_issue" }

func (g GitHubIssue) Send(ctx context.Context, msg Message) error {
	payload, _ := json.Marshal(map[string]string{"title": msg.Subject, "body": msg.Body})
	base := g.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	url := strings.TrimRight(base, "/") + "/repos/" + g.Owner + "/" + g.Repo + "/issues"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("github issue status %d", res.StatusCode)
	}
	return nil
}

// SMTPMailer sends the same message over email.
type SMTPMailer struct {
	Addr string
	From string
	To   []string
	Auth smtp.Auth
}

func (m SMTPMailer) Name() string { return "email" }

func (m SMTPMailer) Send(ctx context.Context, msg Message) error {
	if m.Addr == "" || m.From == "" || len(m.To) == 0 {
		return fmt.Errorf("email delivery is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	body := "Subject: " + strings.ReplaceAll(msg.Subject, "\n", " ") + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + msg.Body
	return smtp.SendMail(m.Addr, m.Auth, m.From, m.To, []byte(body))
}

// Recorder captures messages in tests.
type Recorder struct {
	Messages []Message
}

func (r *Recorder) Name() string { return "recorder" }

func (r *Recorder) Send(_ context.Context, msg Message) error {
	r.Messages = append(r.Messages, msg)
	return nil
}
