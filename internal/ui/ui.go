// Package ui renders the Healbot operations console.
package ui

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/model"
)

// Page is the view model for every console screen.
type Page struct {
	Title      string
	View       string
	Mode       string
	DryRun     bool
	Killed     bool
	Incidents  []model.Incident
	Incident   model.Incident
	Events     []model.AuditEvent
	Pull       fix.Pull
	AdminToken string
}

var pages = template.Must(template.New("page").Funcs(template.FuncMap{
	"when": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.UTC().Format("15:04:05")
	},
	"pct": func(v float64) string {
		return fmt.Sprintf("%.0f%%", v*100)
	},
	"sev":  func(s model.Severity) string { return s.String() },
	"sevn": func(s model.Severity) int { return int(s) },
	"tier": func(t model.Tier) string { return t.String() },
}).Parse(pageHTML))

// Render writes an HTML page.
func Render(w http.ResponseWriter, page Page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.Execute(w, page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
