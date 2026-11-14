package fix

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

const patchSystem = `You are Healbot's patch writer for a production incident.
Respond with one JSON object and no markdown:
{"title":"","summary":"","files":[{"path":"","content":""}]}
content is the full new file, not a unified diff.
Include a regression test that fails on the current code and passes with the fix.
Follow the repository instructions when they are present.
Make the smallest change that fixes the incident. Match the existing layering and error handling.
Do not edit CI workflows, secret files, infrastructure, or lockfiles.
Do not include secrets, tokens, or connection strings.`

type patchDoc struct {
	Title   string      `json:"title"`
	Summary string      `json:"summary"`
	Files   []patchFile `json:"files"`
}

type patchFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s Service) proposeModel(ctx context.Context, inc model.Incident, outcome string, cfg config.Fix) (Draft, error) {
	instructions, err := Discover(ctx, s.Source, "", cfg.InstructionPaths)
	if err != nil {
		return Draft{}, fmt.Errorf("read repository instructions: %w", err)
	}
	instructions = boundInstructions(instructions, cfg.MaxInstructionBytes)
	sources, err := s.fetchSources(ctx, inc, cfg)
	if err != nil {
		return Draft{}, err
	}
	user := patchPrompt(inc, outcome, instructions, sources, cfg.MaxPromptBytes)
	raw, err := s.Model.Complete(ctx, patchSystem, user)
	if err != nil {
		return Draft{}, fmt.Errorf("model: %w", err)
	}
	doc, err := parsePatch(raw)
	if err != nil {
		return Draft{}, err
	}
	files := make([]File, len(doc.Files))
	for i, f := range doc.Files {
		files[i] = File{Path: f.Path, Content: f.Content}
	}
	index := map[string]sourceFile{}
	for _, src := range sources {
		index[src.Path] = src
	}
	checked, err := validatePatch(files, index, cfg)
	if err != nil {
		return Draft{}, err
	}
	title := oneLine(doc.Title, 120)
	if title == "" {
		title = oneLine("fix: "+inc.ExceptionType+" in "+inc.TopFrame, 120)
	}
	body := renderBody(inc, outcome, title, config.FixModeModelPatch)
	if summary := strings.TrimSpace(doc.Summary); summary != "" {
		body += "\n### Model summary\n\n" + boundText(summary, 2000) + "\n"
	}
	body += "\nThe model wrote this patch. Healbot checked the paths and the size, then opened the pull request. It did not merge it.\n"
	if len(instructions) > 0 {
		body += "\nArchitecture files included in the prompt:\n\n"
		for _, item := range instructions {
			body += "- `" + item.Path + "`\n"
		}
	}
	return Draft{
		Title:  title,
		Body:   body,
		Branch: "healbot/" + sanitize(inc.ID),
		Files:  checked,
	}, nil
}

func (s Service) fetchSources(ctx context.Context, inc model.Incident, cfg config.Fix) ([]sourceFile, error) {
	if s.Source == nil {
		return nil, nil
	}
	var out []sourceFile
	frames := extractPaths(inc)
	for _, cands := range frames {
		if len(out) >= cfg.ContextFiles {
			break
		}
		got, ok, err := s.readFirst(ctx, cands, cfg.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, got)
	}
	return out, nil
}

func (s Service) readFirst(ctx context.Context, cands []string, maxBytes int) (sourceFile, bool, error) {
	var lastMissing sourceFile
	saw := false
	for _, cand := range cands {
		content, missing, err := s.Source.Read(ctx, cand, "")
		if err != nil {
			return sourceFile{}, false, fmt.Errorf("read %s: %w", cand, err)
		}
		if missing {
			if !saw {
				lastMissing = sourceFile{Path: cand, Missing: true}
				saw = true
			}
			continue
		}
		file := sourceFile{Path: cand, Content: content}
		if maxBytes > 0 && len(content) > maxBytes {
			file.Content = content[:maxBytes]
			file.Truncated = true
		}
		return file, true, nil
	}
	if saw {
		return lastMissing, true, nil
	}
	return sourceFile{}, false, nil
}

func patchPrompt(inc model.Incident, outcome string, instructions []Instruction, sources []sourceFile, maxBytes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "incident: %s\nresource: %s\nseverity: %s\nexception: %s\nframe: %s\noperation: %s\nmessage: %s\ntier2: %s\n",
		inc.ID, inc.ResourceID, inc.Severity, inc.ExceptionType, inc.TopFrame, inc.Operation, boundText(inc.Message, 500), outcome)
	if inc.Hypothesis != nil {
		fmt.Fprintf(&b, "hypothesis: %s\ncategory: %s\nconfidence: %.2f\n", boundText(inc.Hypothesis.Summary, 800), inc.Hypothesis.Category, inc.Hypothesis.Confidence)
		for _, e := range inc.Hypothesis.Evidence {
			fmt.Fprintf(&b, "evidence: %s\n", boundText(e, 300))
		}
	}
	if inc.StackTrace != "" {
		fmt.Fprintf(&b, "stack:\n%s\n", boundText(inc.StackTrace, 2500))
	}
	if len(instructions) == 0 {
		b.WriteString("repository instructions: none found. Prefer a minimal diff and a regression test.\n")
	}
	for _, item := range instructions {
		fmt.Fprintf(&b, "INSTRUCTION path=%s\n%s\n", item.Path, item.Content)
	}
	for _, src := range sources {
		switch {
		case src.Missing:
			fmt.Fprintf(&b, "SOURCE missing path=%s\n", src.Path)
		case src.Truncated:
			fmt.Fprintf(&b, "SOURCE skipped path=%s reason=too_large\n", src.Path)
		default:
			fmt.Fprintf(&b, "SOURCE path=%s\n%s\n", src.Path, redact(src.Content))
		}
	}
	out := b.String()
	if maxBytes > 0 && len(out) > maxBytes {
		return out[:maxBytes] + "\n[truncated]\n"
	}
	return out
}

func parsePatch(raw string) (patchDoc, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var doc patchDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return patchDoc{}, fmt.Errorf("model response is not the expected JSON object")
	}
	if len(doc.Files) == 0 {
		return patchDoc{}, fmt.Errorf("model returned no files")
	}
	return doc, nil
}
