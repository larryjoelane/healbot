package fix

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

// Service turns a diagnosis into a pull request or an issue.
type Service struct {
	Provider Provider
	Source   Source
	Issues   Issuer
	CI       Checker
	Model    Completer
	Fix      config.Fix
}

// Completer is a text model. Azure OpenAI implements it, and so does DemoModel.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Issuer opens a GitHub issue. It does not merge a pull request.
type Issuer interface {
	CreateIssue(ctx context.Context, draft IssueDraft) (model.IssueRef, error)
}

// Source reads a repository without writing to it.
type Source interface {
	Read(ctx context.Context, path, ref string) (content string, missing bool, err error)
	ListFiles(ctx context.Context, dir, ref string) ([]string, error)
}

// Checker reads commit status and adds labels. It has no merge method.
type Checker interface {
	WaitCI(ctx context.Context, ref model.PullRequestRef, poll time.Duration) (string, error)
	Label(ctx context.Context, ref model.PullRequestRef, labels []string) error
}

// OpenResult is what tier 3 produced. A fallback still opens the template pull request.
type OpenResult struct {
	Ref       model.PullRequestRef
	Files     []model.ProposedFile
	Issue     *model.IssueRef
	Mode      string
	Requested string
	Fallback  string
	Warning   string
}

// Open selects the tier-3 mode and opens a pull request or an issue.
// It never merges. res.FixMode and res.FixAgent override the process config
// when they are set.
func (s Service) Open(ctx context.Context, inc model.Incident, tier2Outcome string, res config.Resource) (OpenResult, error) {
	settings := s.Fix.Normalized()
	raw := settings.Mode
	if strings.TrimSpace(res.FixMode) != "" {
		raw = res.FixMode
	}
	mode, err := config.NormalizeFixMode(raw)
	if err != nil {
		out, oerr := s.openTemplate(ctx, inc, tier2Outcome, settings, err.Error())
		out.Requested = raw
		out.Fallback = redact(err.Error())
		out.Mode = config.FixModeTemplate
		return out, oerr
	}
	switch mode {
	case config.FixModeModelPatch:
		return s.openModel(ctx, inc, tier2Outcome, settings)
	case config.FixModeIssue:
		return s.openIssue(ctx, inc, tier2Outcome, settings, res)
	default:
		out, oerr := s.openTemplate(ctx, inc, tier2Outcome, settings, "")
		out.Requested = config.FixModeTemplate
		return out, oerr
	}
}

func (s Service) openModel(ctx context.Context, inc model.Incident, outcome string, settings config.Fix) (OpenResult, error) {
	if s.Model == nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeModelPatch, "no model configured")
	}
	draft, err := s.proposeModel(ctx, inc, outcome, settings)
	if err != nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeModelPatch, err.Error())
	}
	ref, files, warning, err := s.commit(ctx, draft, settings, config.FixModeModelPatch)
	if err != nil {
		return OpenResult{Requested: config.FixModeModelPatch, Mode: config.FixModeModelPatch, Warning: warning}, err
	}
	return OpenResult{
		Ref: ref, Files: files, Mode: config.FixModeModelPatch, Requested: config.FixModeModelPatch, Warning: warning,
	}, nil
}

func (s Service) openIssue(ctx context.Context, inc model.Incident, outcome string, settings config.Fix, res config.Resource) (OpenResult, error) {
	agentRaw := settings.Issue.Agent
	if strings.TrimSpace(res.FixAgent) != "" {
		agentRaw = res.FixAgent
	}
	agent, err := config.NormalizeFixAgent(agentRaw)
	if err != nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeIssue, err.Error())
	}
	if s.Issues == nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeIssue, "issue mode requires a GitHub provider")
	}
	instructions, err := Discover(ctx, s.Source, "", settings.InstructionPaths)
	if err != nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeIssue, err.Error())
	}
	draft := buildIssue(inc, outcome, boundInstructions(instructions, settings.MaxInstructionBytes), agent, settings.Issue)
	ref, err := s.Issues.CreateIssue(ctx, draft)
	if ref.ID != "" {
		out := OpenResult{Issue: &ref, Mode: config.FixModeIssue, Requested: config.FixModeIssue}
		if err != nil {
			out.Warning = redact(err.Error())
		}
		return out, nil
	}
	if err != nil {
		return s.fallback(ctx, inc, outcome, settings, config.FixModeIssue, err.Error())
	}
	return s.fallback(ctx, inc, outcome, settings, config.FixModeIssue, "issue provider returned an empty id")
}

func (s Service) fallback(ctx context.Context, inc model.Incident, outcome string, settings config.Fix, requested, reason string) (OpenResult, error) {
	reason = redact(strings.TrimSpace(reason))
	out, err := s.openTemplate(ctx, inc, outcome, settings, reason)
	out.Requested = requested
	out.Fallback = reason
	out.Mode = config.FixModeTemplate
	return out, err
}

func (s Service) openTemplate(ctx context.Context, inc model.Incident, outcome string, settings config.Fix, note string) (OpenResult, error) {
	draft := Propose(inc, outcome)
	if strings.TrimSpace(note) != "" {
		draft = proposeNoted(inc, outcome, redact(note))
	}
	ref, files, warning, err := s.commit(ctx, draft, settings, config.FixModeTemplate)
	return OpenResult{
		Ref: ref, Files: files, Mode: config.FixModeTemplate, Requested: config.FixModeTemplate, Warning: warning,
	}, err
}

func (s Service) commit(ctx context.Context, draft Draft, settings config.Fix, mode string) (model.PullRequestRef, []model.ProposedFile, string, error) {
	if s.Provider == nil {
		return model.PullRequestRef{}, nil, "", fmt.Errorf("no repository provider configured")
	}
	ref, err := s.Provider.Create(ctx, draft)
	if err != nil {
		return model.PullRequestRef{}, nil, "", err
	}
	ref.Mode = mode
	ref, warning := s.finish(ctx, ref, settings)
	files := make([]model.ProposedFile, len(draft.Files))
	for i, f := range draft.Files {
		files[i] = model.ProposedFile{Path: f.Path, Content: f.Content}
	}
	return ref, files, warning, nil
}

func (s Service) finish(ctx context.Context, ref model.PullRequestRef, settings config.Fix) (model.PullRequestRef, string) {
	if !settings.WaitForCI || s.CI == nil {
		return ref, ""
	}
	if ref.SHA == "" {
		if _, ok := s.CI.(*Memory); !ok {
			return ref, "ci skipped: commit sha was not returned"
		}
	}
	poll := settings.CIPoll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	wait := settings.CITimeout
	if wait <= 0 {
		wait = 2 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	state, err := s.CI.WaitCI(cctx, ref, poll)
	if state == "" {
		state = "timeout"
	}
	ref.CI = state
	if err != nil {
		return ref, redact(err.Error())
	}
	var label string
	switch state {
	case "success":
		label = settings.SuccessLabel
	case "failure", "error":
		label = settings.FailureLabel
	}
	if label == "" {
		return ref, ""
	}
	if err := s.CI.Label(ctx, ref, []string{label}); err != nil {
		return ref, "ci label: " + redact(err.Error())
	}
	return ref, ""
}

func proposeNoted(inc model.Incident, outcome, note string) Draft {
	d := Propose(inc, outcome)
	if strings.TrimSpace(note) == "" {
		return d
	}
	d.Body += "\n### Mode fallback\n\n" + note + "\n"
	return d
}
