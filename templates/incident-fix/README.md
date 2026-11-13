# Incident-fix instructions

Copy this directory onto the root of the repository Healbot should patch. `AGENTS.md` is the only copy of the rules. The other files point at it so Copilot and Claude Code do not drift apart.

| Copy from here | Place at the target root |
| --- | --- |
| `AGENTS.md` | `AGENTS.md` |
| `CLAUDE.md` | `CLAUDE.md` |
| `.github/copilot-instructions.md` | `.github/copilot-instructions.md` |
| `.github/agents/incident-fix.agent.md` | `.github/agents/incident-fix.agent.md` |
| `.github/skills/incident-fix/SKILL.md` | `.github/skills/incident-fix/SKILL.md` |
| `.claude/skills/incident-fix/SKILL.md` | `.claude/skills/incident-fix/SKILL.md` |
| `.claude/agents/incident-fixer.md` | `.claude/agents/incident-fixer.md` |

Healbot reads these paths when `fix.mode` is `model_patch` or `issue`. The prompt and the issue body prefer `AGENTS.md`.

## Copilot

- Coding agent loads `AGENTS.md` and `.github/copilot-instructions.md`.
- To select the custom agent, set Healbot `fix.issue.custom_agent` to `incident-fix`.
- Assignment uses the GitHub issue API and the login `copilot-swe-agent[bot]`. Permissions are in `docs/fix.md` in the Healbot repository.

## Claude Code

- `CLAUDE.md` imports `AGENTS.md` with `@AGENTS.md`. Claude Code does not read `AGENTS.md` unless something imports it.
- The skill is `.claude/skills/incident-fix/SKILL.md`. Invoke it as `/incident-fix`.
- The subagent is `.claude/agents/incident-fixer.md`.
- A GitHub Action that comments when it sees `@claude` is the handoff for `fix.issue.agent: claude`. The workflow belongs in the target repository. See `docs/fix.md`.
