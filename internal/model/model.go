// Package model holds the incident domain used by every tier of Healbot.
package model

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Severity follows the Azure Monitor scale. A smaller number is more severe.
type Severity int

const (
	Sev0 Severity = 0
	Sev1 Severity = 1
	Sev2 Severity = 2
	Sev3 Severity = 3
	Sev4 Severity = 4
)

// ParseSeverity accepts Sev2, sev2, and the bare digit.
func ParseSeverity(s string) (Severity, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "sev")
	switch s {
	case "0", "critical":
		return Sev0, true
	case "1", "error":
		return Sev1, true
	case "2", "high":
		return Sev2, true
	case "3", "warning", "warn":
		return Sev3, true
	case "4", "info", "informational", "verbose":
		return Sev4, true
	default:
		return 0, false
	}
}

func (s Severity) String() string {
	return "sev" + strconv.Itoa(int(s))
}

func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *Severity) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case string:
		parsed, ok := ParseSeverity(v)
		if !ok {
			return strconv.ErrSyntax
		}
		*s = parsed
	case float64:
		*s = Severity(v)
	default:
		return strconv.ErrSyntax
	}
	return nil
}

// Worse reports whether s is at least as severe as threshold.
// Sev0 is worse than Sev4.
func (s Severity) Worse(threshold Severity) bool {
	return s <= threshold
}

// Source identifies which ingest path produced a signal.
type Source string

const (
	SourceMonitorAlert  Source = "monitor_alert"
	SourceException     Source = "appinsights_exception"
	SourceFailedRequest Source = "appinsights_request"
	SourceLogAnalytics  Source = "log_analytics"
	SourceLogBatch      Source = "log_batch"
)

// Signal is one normalized error observation. Buf owns the bytes behind the
// string fields when the signal came from the low-allocation log parser.
// Callers must not reuse Buf while those strings are still referenced.
type Signal struct {
	ID            string            `json:"id"`
	Source        Source            `json:"source"`
	ReceivedAt    time.Time         `json:"received_at"`
	OccurredAt    time.Time         `json:"occurred_at"`
	Severity      Severity          `json:"severity"`
	ResourceID    string            `json:"resource_id"`
	ResourceName  string            `json:"resource_name"`
	ResourceType  string            `json:"resource_type"`
	Operation     string            `json:"operation,omitempty"`
	Message       string            `json:"message"`
	ExceptionType string            `json:"exception_type,omitempty"`
	StackTrace    string            `json:"stack_trace,omitempty"`
	TopFrame      string            `json:"top_frame,omitempty"`
	ErrorRate     float64           `json:"error_rate,omitempty"`
	Resolved      bool              `json:"resolved,omitempty"`
	Fingerprint   uint64            `json:"-"`
	Properties    map[string]string `json:"properties,omitempty"`
	Buf           []byte            `json:"-"`
}

// Status is the incident lifecycle state.
type Status string

const (
	StatusOpen          Status = "open"
	StatusDiagnosed     Status = "diagnosed"
	StatusRemediating   Status = "remediating"
	StatusResolved      Status = "resolved"
	StatusEscalated     Status = "escalated"
	StatusPROpened      Status = "pr_opened"
	StatusAwaitingHuman Status = "awaiting_human"
	StatusSuppressed    Status = "suppressed"
)

// Tier is the escalation level that has been reached.
type Tier int

const (
	TierNone      Tier = 0
	TierDetect    Tier = 1
	TierRemediate Tier = 2
	TierCodeFix   Tier = 3
)

func (t Tier) String() string {
	switch t {
	case TierDetect:
		return "detect"
	case TierRemediate:
		return "remediate"
	case TierCodeFix:
		return "code-fix"
	default:
		return "none"
	}
}

// Category is the diagnoser's classification of the failure.
type Category string

const (
	CategoryCode       Category = "code"
	CategoryDeployment Category = "deployment"
	CategoryCapacity   Category = "capacity"
	CategoryDependency Category = "dependency"
	CategoryConfig     Category = "config"
	CategoryUnknown    Category = "unknown"
)

// ActionKind is a reversible runbook the tier-2 executor is allowed to plan.
type ActionKind string

const (
	ActionRestart          ActionKind = "restart"
	ActionScaleOut         ActionKind = "scale_out"
	ActionSlotSwap         ActionKind = "slot_swap"
	ActionRestartRevision  ActionKind = "restart_revision"
	ActionRollbackRevision ActionKind = "rollback_revision"
	ActionRestartConsumer  ActionKind = "restart_consumer"
)

// Valid reports whether the kind is part of the closed runbook catalog.
func (k ActionKind) Valid() bool {
	switch k {
	case ActionRestart, ActionScaleOut, ActionSlotSwap, ActionRestartRevision, ActionRollbackRevision, ActionRestartConsumer:
		return true
	default:
		return false
	}
}

// Hypothesis is a root-cause theory. It is not an instruction to mutate.
type Hypothesis struct {
	Summary         string     `json:"summary"`
	Confidence      float64    `json:"confidence"`
	Category        Category   `json:"category"`
	LikelyCode      bool       `json:"likely_code"`
	SuggestedAction ActionKind `json:"suggested_action,omitempty"`
	Evidence        []string   `json:"evidence,omitempty"`
	Provider        string     `json:"provider"`
}

// ActionRecord is one tier-2 plan or execution.
type ActionRecord struct {
	ID         string     `json:"id"`
	Kind       ActionKind `json:"kind"`
	DryRun     bool       `json:"dry_run"`
	Outcome    string     `json:"outcome"`
	Detail     string     `json:"detail"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

// ProposedFile is a file the tier-3 proposer wants a human to review.
type ProposedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// PullRequestRef points at a pull request. Healbot never merges it.
type PullRequestRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Branch   string `json:"branch"`
}

// Incident is a deduplicated group of signals plus everything the tiers did.
type Incident struct {
	ID            string          `json:"id"`
	Fingerprint   string          `json:"fingerprint"`
	Status        Status          `json:"status"`
	Severity      Severity        `json:"severity"`
	ResourceID    string          `json:"resource_id"`
	ResourceName  string          `json:"resource_name"`
	ResourceType  string          `json:"resource_type"`
	Title         string          `json:"title"`
	Operation     string          `json:"operation,omitempty"`
	ExceptionType string          `json:"exception_type,omitempty"`
	Message       string          `json:"message"`
	StackTrace    string          `json:"stack_trace,omitempty"`
	TopFrame      string          `json:"top_frame,omitempty"`
	ErrorRate     float64         `json:"error_rate,omitempty"`
	FirstSeen     time.Time       `json:"first_seen"`
	LastSeen      time.Time       `json:"last_seen"`
	Count         int             `json:"count"`
	SignalIDs     []string        `json:"signal_ids,omitempty"`
	Hypothesis    *Hypothesis     `json:"hypothesis,omitempty"`
	TierReached   Tier            `json:"tier_reached"`
	Suppressed    bool            `json:"suppressed"`
	Actions       []ActionRecord  `json:"actions,omitempty"`
	PullRequest   *PullRequestRef `json:"pull_request,omitempty"`
	ProposedFiles []ProposedFile  `json:"proposed_files,omitempty"`
}

// Deployment is a recent change Healbot can roll back toward.
type Deployment struct {
	Name     string    `json:"name"`
	Revision string    `json:"revision,omitempty"`
	Slot     string    `json:"slot,omitempty"`
	At       time.Time `json:"at"`
	Active   bool      `json:"active"`
}

// ContextBundle is the read-only evidence gathered before a decision.
type ContextBundle struct {
	Deployments  []Deployment `json:"deployments,omitempty"`
	RecentDeploy bool         `json:"recent_deploy"`
	Logs         []string     `json:"logs,omitempty"`
}

// HealthStatus is the post-action check. Healthy means the symptom eased.
type HealthStatus struct {
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

// AuditEvent is one durable decision or action.
type AuditEvent struct {
	ID         string            `json:"id"`
	Time       time.Time         `json:"time"`
	IncidentID string            `json:"incident_id,omitempty"`
	Kind       string            `json:"kind"`
	Actor      string            `json:"actor"`
	Message    string            `json:"message"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// Audit kinds. Decision events are the safety record.
const (
	AuditSignalReceived      = "signal.received"
	AuditIncidentOpened      = "incident.opened"
	AuditIncidentGrouped     = "incident.grouped"
	AuditIncidentResolved    = "incident.resolved"
	AuditDiagnosisCompleted  = "diagnosis.completed"
	AuditPolicyDecision      = "policy.decision"
	AuditNotifySent          = "notify.sent"
	AuditNotifyFailed        = "notify.failed"
	AuditRemediationDryRun   = "remediation.dry_run"
	AuditRemediationExecuted = "remediation.executed"
	AuditRemediationBlocked  = "remediation.blocked"
	AuditVerification        = "verification.result"
	AuditEscalated           = "escalation.tier3"
	AuditPROpened            = "pr.opened"
	AuditPRFailed            = "pr.failed"
	AuditKillSwitch          = "kill_switch.changed"
	AuditSuppressed          = "incident.suppressed"
	AuditAutomationBlocked   = "automation.blocked"
	AuditAuditSaturated      = "audit.saturated"
)

// Known Azure resource type strings.
const (
	TypeWebSite      = "Microsoft.Web/sites"
	TypeServerFarm   = "Microsoft.Web/serverfarms"
	TypeContainerApp = "Microsoft.App/containerApps"
)
