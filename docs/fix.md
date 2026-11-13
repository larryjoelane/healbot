# Tier 3 fix modes

Tier 3 runs only after policy allows it. It never merges a pull request. The mode decides what it opens.

`template` is the default. It is the same pull request Healbot already opened: a patch and a regression-test sketch generated from the stack frame, with no model call and no agent assignment. That stays the default because a model patch can be wrong and an agent assignment needs a paid coding agent plus a user token. Both are opt-in.

## Precedence

1. Resource `fix_mode` / `fix_agent`, when set.
2. Otherwise `HEALBOT_FIX_MODE` / `HEALBOT_FIX_AGENT`, when set.
3. Otherwise `fix.mode` / `fix.issue.agent` in YAML.
4. Otherwise `template` and, for an issue, `copilot`.

A resource can opt into `model_patch` or back to `template` while the process default stays put. The path denylist is not per resource. One service cannot turn off the CI, secret, infrastructure, or lockfile rules.

`copilot_issue` is an alias of `issue`.

## template

`fix.Service` builds the branch with `Propose` and the configured provider creates the pull request. GitHub and Azure DevOps implement `Create` only. The in-memory provider does the same in demo mode.

## model_patch

1. Read architecture files from the target repository (see below).
2. Read the files named by the stack trace, up to `context_files`.
3. Redact obvious tokens and connection strings, then trim the prompt to `max_prompt_bytes`.
4. Call the model. Production uses the same Azure OpenAI client as diagnosis (`llm.endpoint`, `llm.deployment`, managed identity unless `llm.api_key_env` is set). The `Complete` method is the plug point: another provider implements the same method and is assigned to `fix.Service.Model`. Demo mode never calls Azure. It uses an in-process stand-in so the path runs with no network. That stand-in is not a language-aware repair.
5. Parse one JSON object: a title, a summary, and full file contents. A unified diff is rejected.
6. Validate, then open the pull request. When `wait_for_ci` is true, poll check runs (or the combined commit status) and add `success_label` or `failure_label`. The pull request stays open either way.
7. On any model, discovery, or validation failure, open the template pull request instead and audit `fix.fallback`. A provider error after a valid patch is `pr.failed` and does not write a second proposal.

Validation rejects:

- paths that are absolute, empty, or contain `..` or `.git`
- paths outside `allow_paths` when that list is set
- CI workflows (`.github/workflows`, `.github/actions`, Azure Pipelines, GitLab CI, Jenkins, CircleCI)
- secret-like paths (`.env`, `*secret*`, `*credential*`, keys and certificates)
- infrastructure (`*.tf`, `*.tfvars`, `*.bicep`, `terraform/**`)
- lockfiles (`go.sum`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, and the other names in `defaultDeny`)
- binaries (`.exe`, `.dll`, `.so`, `.dylib`)
- a patch with no regression test, an empty file, a file over `max_file_bytes`, or a patch over `max_patch_bytes`
- content that still matches the token redactor

`allow_denied_paths` is the explicit exception. Listing `go.sum` there allows that file and nothing else on the denylist.

## issue

This mode does not open a pull request. It opens a GitHub issue with the diagnosis, evidence, stack, affected paths, reproduction steps, and acceptance criteria that include a regression test. It links the architecture files it found and quotes a short excerpt of `AGENTS.md` when that file exists.

`fix.issue.agent` chooses the handoff:

| Agent | What Healbot does |
| --- | --- |
| `copilot` | Creates the issue assigned to `fix.issue.assignee` (default `copilot-swe-agent[bot]`) and sends `agent_assignment` (`target_repo`, `base_branch`, `custom_instructions`, `custom_agent`). |
| `claude` | Puts `fix.issue.mention` (default `@claude`) in the issue body and in a follow-up comment so a Claude Code GitHub Action can see it. It does not assign Copilot. |
| `none` | Creates the issue only. Add `fix.issue.labels` if a workflow should match a label. |

If the create call rejects the assignee preview fields, Healbot retries without them, then `POST /issues/{n}/assignees` with `agent_assignment`. If that fails, it uses GraphQL `replaceActorsForAssignable` with the `GraphQL-Features: coding_agent_model_selection,issues_copilot_assignment_api_support` header, after looking up the bot id from `suggestedActors(capabilities: [CAN_BE_ASSIGNED])`. The documented login is `copilot-swe-agent`. Healbot sends `copilot-swe-agent[bot]` and accepts either form.

Issue mode needs the GitHub provider. On Azure DevOps it falls back to the template pull request and audits that reason. An issue that was created is kept even if the later assignment or comment fails; that failure is `fix.warning`, not a second pull request.

### Copilot token

GitHub documents this assignment as a user-to-server token: a fine-grained or classic personal access token, an OAuth token, or a GitHub App user-to-server token. A GitHub App installation token is not accepted. Copilot cloud agent must be enabled for the repository and the account needs a paid Copilot plan that includes it.

Fine-grained token, from the current docs:

| Permission | Access |
| --- | --- |
| Metadata | Read |
| Actions | Read and write |
| Contents | Read and write |
| Issues | Read and write |
| Pull requests | Read and write |

A classic token needs the `repo` scope. Healbot uses one token, `repo.github.token_env` (default `GITHUB_TOKEN`), for contents, pull requests, issues, and CI status. `model_patch` with `wait_for_ci` reads check runs. Labeling uses the issues API. Give the token the union of those permissions. Do not commit the token.

`fix.issue.custom_agent` is the Copilot custom agent name (the file name without `.md` / `.agent.md`), for example `incident-fix` when the repository has `.github/agents/incident-fix.agent.md`.

### Claude Code GitHub Action

The current action is `anthropics/claude-code-action`. The documented interactive workflow runs on `issue_comment` when the comment body contains `@claude`. Healbot posts that comment, and also puts the mention in the issue body for a workflow that triggers on `issues: [opened]`. A label such as `claude` is optional; set `fix.issue.labels` and match it in the workflow if you prefer a label to a mention.

The workflow lives in the target repository, not in Healbot. It needs `contents: write`, `pull-requests: write`, `issues: write`, `id-token: write`, and `actions: read`, plus the action's API key secret. Checkout the repository before the action if it should load `.claude/skills`. Healbot's own token only needs issues read/write to create the issue and the comment. The action's token is what opens the pull request. Neither token is allowed to be merged by Healbot; the action should be instructed not to merge, which the issue body already says.

Example shape, with the secret kept in the repository's Actions secret store:

```yaml
name: Claude Code
on:
  issue_comment:
    types: [created]
  issues:
    types: [opened]
jobs:
  claude:
    if: >
      (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@claude')) ||
      (github.event_name == 'issues' && contains(github.event.issue.body, '@claude'))
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
      issues: write
      id-token: write
      actions: read
    steps:
      - uses: actions/checkout@v4
      - uses: anthropics/claude-code-action@v1
        with:
          anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}
          prompt: "/incident-fix"
```

`/incident-fix` runs the skill in `.claude/skills/incident-fix/` after checkout. Leave the prompt empty to let the mention and the issue body drive the task.

## Architecture guide

One file holds the rules: `AGENTS.md` at the repository root. Copilot coding agent reads `AGENTS.md` on its own. Claude Code reads `CLAUDE.md`, not `AGENTS.md`, so the template `CLAUDE.md` is only:

```markdown
@AGENTS.md
```

Claude Code imports that path at session start. Edit `AGENTS.md` when the rules change. Do not copy them into the other files.

Thin entry points, all of which tell the agent to follow `AGENTS.md`:

| Tool | Path |
| --- | --- |
| Copilot repository instructions | `.github/copilot-instructions.md` |
| Copilot custom agent | `.github/agents/incident-fix.agent.md` |
| Copilot skill | `.github/skills/incident-fix/SKILL.md` |
| Claude Code skill | `.claude/skills/incident-fix/SKILL.md` |
| Claude Code subagent | `.claude/agents/incident-fixer.md` |

The skill and subagent files use the current frontmatter (`name`, `description`, and for a subagent `tools`). Healbot's discovery reads those paths, plus `.claude/CLAUDE.md` and `.github/instructions/*.md`, and any extra `fix.instruction_paths`. `model_patch` puts the file contents in the prompt, `AGENTS.md` first, and stops at `max_instruction_bytes`. The issue body lists the paths and excerpts the canonical file.

`templates/incident-fix/` is the copy-ready tree. It is generic: layering, error handling, test conventions, a minimal diff, and a regression test. It names no company or product beyond the agent file names required to load the instructions.

## Config keys

```yaml
fix:
  mode: template                 # template | model_patch | issue
  max_files: 6                   # hard cap 20
  max_file_bytes: 24000          # hard cap 200000
  max_patch_bytes: 64000         # hard cap 500000
  max_prompt_bytes: 48000        # hard cap 200000
  max_instruction_bytes: 12000   # hard cap 48000
  context_files: 4               # hard cap 20
  wait_for_ci: false
  ci_timeout: 2m
  ci_poll: 5s
  success_label: healbot:ci-passed
  failure_label: healbot:ci-failed
  allow_paths: []
  extra_deny_paths: []
  allow_denied_paths: []
  instruction_paths: []
  issue:
    agent: copilot
    assignee: copilot-swe-agent[bot]
    mention: "@claude"
    labels: []
    base_branch: ""
    custom_agent: ""
    custom_instructions: ""
```

Environment overrides: `HEALBOT_FIX_MODE`, `HEALBOT_FIX_AGENT`, `HEALBOT_FIX_ASSIGNEE`, `HEALBOT_FIX_MENTION`, `HEALBOT_FIX_WAIT_CI`, `HEALBOT_FIX_CUSTOM_AGENT`, `HEALBOT_FIX_LABELS` (comma-separated), `HEALBOT_FIX_BASE_BRANCH`, `HEALBOT_FIX_ALLOW_DENIED` (comma-separated, appended).

## App settings

`deploy/main.bicep` on `main` does not set these variables. Pull request #2 adds Terraform for App Service and is not merged; its app settings do not include them either. After that change lands, add the `HEALBOT_FIX_*` names above as application settings if you need a mode other than the default. Unset, or `HEALBOT_FIX_MODE=template`, keeps the safe behavior. Do not store `GITHUB_TOKEN` in the YAML file. Inject it as a secret reference the same way the webhook token is injected.

## Audit

| Kind | When |
| --- | --- |
| `pr.opened` | A pull request was created. `mode` is the mode that produced it. |
| `issue.opened` | An issue was created. |
| `fix.fallback` | The requested mode failed and the template pull request was used. |
| `fix.warning` | The issue or pull request exists, and a later step (assignment, comment, CI label) failed. |
| `fix.ci` | `wait_for_ci` observed a state. |
| `pr.failed` | The provider failed and nothing was opened. |

## What this repository cannot prove

Live Copilot assignment, a real Claude Code Action run, and a real Azure OpenAI call were not executed here. Tests use an in-memory repository, a fake model, and `httptest` servers for the GitHub and Azure DevOps APIs. Demo mode is the offline exercise of all three modes.
