package fix

import (
	"fmt"
	"strings"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

// IssueDraft is the GitHub issue handed to an agent. Healbot does not merge
// anything the agent later opens.
type IssueDraft struct {
	Title              string
	Body               string
	Comment            string
	Agent              string
	Assignee           string
	Mention            string
	Labels             []string
	BaseBranch         string
	CustomAgent        string
	CustomInstructions string
}

func buildIssue(inc model.Incident, outcome string, instructions []Instruction, agent string, issue config.FixIssue) IssueDraft {
	exc := inc.ExceptionType
	if exc == "" {
		exc = "the exception"
	}
	frame := inc.TopFrame
	if frame == "" {
		frame = "the failing frame"
	}
	title := oneLine("fix: "+exc+" in "+frame, 120)
	var b strings.Builder
	b.WriteString("## Diagnosis\n\n")
	if inc.Hypothesis != nil {
		fmt.Fprintf(&b, "%s\n\nConfidence %.0f%% via %s. Category `%s`.\n\n",
			boundText(inc.Hypothesis.Summary, 2000), inc.Hypothesis.Confidence*100, inc.Hypothesis.Provider, inc.Hypothesis.Category)
	} else {
		b.WriteString("No hypothesis was recorded.\n\n")
	}
	fmt.Fprintf(&b, "- Incident: `%s`\n- Resource: `%s`\n- Severity: `%s`\n- Operation: `%s`\n- Tier 2 outcome: `%s`\n\n",
		inc.ID, inc.ResourceID, inc.Severity, inc.Operation, outcome)
	if inc.Hypothesis != nil && len(inc.Hypothesis.Evidence) > 0 {
		b.WriteString("## Evidence\n\n")
		for _, e := range inc.Hypothesis.Evidence {
			b.WriteString("- ")
			b.WriteString(boundText(e, 500))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Stack\n\n")
	if inc.StackTrace != "" {
		b.WriteString("```\n")
		b.WriteString(boundText(inc.StackTrace, 4000))
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("No stack trace was recorded. The top frame is `" + frame + "`.\n\n")
	}
	b.WriteString("## Affected files\n\n")
	frames := extractPaths(inc)
	if len(frames) == 0 {
		b.WriteString("No repository path could be read from the stack. Start from the top frame `" + frame + "`.\n\n")
	} else {
		for _, cands := range frames {
			b.WriteString("- `")
			b.WriteString(cands[0])
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Reproduction\n\n")
	fmt.Fprintf(&b, "1. Use the deployment that was active when incident `%s` fired.\n", inc.ID)
	if inc.Operation != "" {
		fmt.Fprintf(&b, "2. Repeat `%s` with the input that produced `%s`.\n", inc.Operation, exc)
	} else {
		fmt.Fprintf(&b, "2. Repeat the failing operation that produced `%s`.\n", exc)
	}
	fmt.Fprintf(&b, "3. Confirm the failure is `%s` at `%s`.\n\n", exc, frame)
	b.WriteString("## Acceptance criteria\n\n")
	b.WriteString("- [ ] A regression test fails on the current code and passes with the fix.\n")
	b.WriteString("- [ ] The test uses this repository's existing test framework and lives next to the other tests for that package.\n")
	fmt.Fprintf(&b, "- [ ] `%s` no longer escapes `%s` for the reported input.\n", exc, frame)
	b.WriteString("- [ ] The diff follows the repository architecture: same layering, same error handling, no drive-by refactors.\n")
	b.WriteString("- [ ] CI workflows, secret files, infrastructure, and lockfiles are unchanged unless the incident is specifically about that file.\n")
	b.WriteString("- [ ] The pull request is left for a human to merge. Do not merge it.\n\n")
	b.WriteString("## Architecture\n\n")
	if len(instructions) == 0 {
		b.WriteString("No `AGENTS.md`, `CLAUDE.md`, Copilot instructions, or skill file was found in the repository. Copy `templates/incident-fix/` from Healbot onto the repository root so agents share one architecture guide.\n\n")
	} else {
		b.WriteString("Follow the canonical guide. Copilot reads `AGENTS.md` directly. Claude Code reads it through `CLAUDE.md` (`@AGENTS.md`). Do not invent a second copy of the rules.\n\n")
		for _, item := range instructions {
			fmt.Fprintf(&b, "- `%s`\n", item.Path)
		}
		if primary, ok := primaryInstruction(instructions); ok {
			b.WriteString("\nExcerpt of `")
			b.WriteString(primary.Path)
			b.WriteString("`:\n\n")
			b.WriteString(boundText(primary.Content, 2000))
			b.WriteString("\n\n")
		}
	}
	mention := issue.Mention
	comment := ""
	assignee := ""
	switch agent {
	case config.FixAgentCopilot:
		assignee = issue.Assignee
		b.WriteString("## Handoff\n\n")
		b.WriteString("This issue is assigned to the GitHub Copilot coding agent")
		if assignee != "" {
			b.WriteString(" (`" + assignee + "`)")
		}
		b.WriteString(". Healbot will not merge the pull request it opens.\n")
		if issue.CustomAgent != "" {
			fmt.Fprintf(&b, "\nCustom agent: `%s`.\n", issue.CustomAgent)
		}
	case config.FixAgentClaude:
		if strings.TrimSpace(mention) == "" {
			mention = "@claude"
		}
		b.WriteString("## Handoff\n\n")
		fmt.Fprintf(&b, "%s Fix this incident. Follow `AGENTS.md` and the `incident-fix` skill when this repository has them. Open a pull request and do not merge it.\n", mention)
		comment = mention + "\n\nFix this incident. Follow AGENTS.md if it is present, and the incident-fix skill if this repository has `.claude/skills/incident-fix/SKILL.md`. Add a regression test. Open a pull request and do not merge it.\n"
	default:
		b.WriteString("## Handoff\n\nNo coding agent is assigned. A person can take this issue, or a repository workflow can pick it up from the label.\n")
	}
	instructionsText := issue.CustomInstructions
	if strings.TrimSpace(instructionsText) == "" && agent == config.FixAgentCopilot {
		instructionsText = "Follow AGENTS.md. Add a regression test. Open a pull request and do not merge it."
	}
	return IssueDraft{
		Title:              title,
		Body:               b.String(),
		Comment:            comment,
		Agent:              agent,
		Assignee:           assignee,
		Mention:            mention,
		Labels:             append([]string(nil), issue.Labels...),
		BaseBranch:         issue.BaseBranch,
		CustomAgent:        issue.CustomAgent,
		CustomInstructions: boundText(instructionsText, 1500),
	}
}
