package fix

import (
	"context"
	"sort"
	"strings"
)

// Instruction is one architecture or agent file from the target repository.
type Instruction struct {
	Path    string
	Content string
}

// Discover reads the instruction files coding agents already look for.
// AGENTS.md is the canonical body. CLAUDE.md, Copilot instructions, skills,
// and custom agents are included when the repository has them.
func Discover(ctx context.Context, src Source, ref string, extra []string) ([]Instruction, error) {
	if src == nil {
		return nil, nil
	}
	var out []Instruction
	seen := map[string]bool{}
	add := func(p string) error {
		p = strings.TrimPrefix(strings.TrimSpace(p), "/")
		if p == "" || seen[p] {
			return nil
		}
		seen[p] = true
		content, missing, err := src.Read(ctx, p, ref)
		if err != nil {
			return err
		}
		if missing {
			return nil
		}
		out = append(out, Instruction{Path: p, Content: content})
		return nil
	}
	for _, p := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		".claude/CLAUDE.md",
		".github/copilot-instructions.md",
	} {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, dir := range []string{
		".github/agents",
		".github/skills",
		".github/instructions",
		".claude/skills",
		".claude/agents",
	} {
		paths, err := src.ListFiles(ctx, dir, ref)
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
		for _, p := range paths {
			if instructionFile(p) {
				if err := add(p); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, p := range extra {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := instructionRank(out[i].Path), instructionRank(out[j].Path)
		if ri != rj {
			return ri < rj
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func instructionFile(p string) bool {
	low := strings.ToLower(p)
	return strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".agent.md")
}

func instructionRank(p string) int {
	switch strings.ToLower(p) {
	case "agents.md":
		return 0
	case "claude.md":
		return 1
	case ".claude/claude.md":
		return 2
	case ".github/copilot-instructions.md":
		return 3
	default:
		low := strings.ToLower(p)
		switch {
		case strings.HasSuffix(low, "skill.md"):
			return 4
		case strings.Contains(low, "/agents/"):
			return 5
		default:
			return 6
		}
	}
}

func boundInstructions(items []Instruction, total int) []Instruction {
	if total < 1 {
		total = 12_000
	}
	per := 8_000
	if per > total {
		per = total
	}
	var out []Instruction
	used := 0
	for _, it := range items {
		if used >= total {
			break
		}
		body := redact(it.Content)
		if len(body) > per {
			body = body[:per] + "\n[truncated]\n"
		}
		if used+len(body) > total {
			keep := total - used
			if keep < 1 {
				break
			}
			body = body[:keep] + "\n[truncated]\n"
		}
		used += len(body)
		it.Content = body
		out = append(out, it)
	}
	return out
}

func primaryInstruction(items []Instruction) (Instruction, bool) {
	for _, it := range items {
		if strings.EqualFold(it.Path, "AGENTS.md") {
			return it, true
		}
	}
	if len(items) == 0 {
		return Instruction{}, false
	}
	return items[0], true
}
