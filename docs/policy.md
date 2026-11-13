# Tier policy

Policy is the YAML under `escalation` and `resources`. The engine is `internal/policy`. A resource that does not match an `id` (or an `id` ending in `*`) is diagnose-only: `max_tier: 1`, no actions.

## Resource fields

| Field | Meaning |
| --- | --- |
| `id` | Azure resource ID. A trailing `*` is a prefix match. |
| `max_tier` | 1 diagnose, 2 remediate, 3 open a pull request. |
| `dry_run` | Omitted means dry run. Ignored when the process dry run is on. |
| `allowed_actions` | Subset of the runbook catalog. |
| `cooldown` | Minimum time between live actions. Falls back to `safety.cooldown`. |
| `max_actions_per_hour` | Live attempts per hour. |
| `max_instances` | Ceiling for `scale_out`. |
| `known_good_slot` | Slot swapped back with production. Default `staging` for sites. |
| `known_good_revision` | Container revision to activate. Empty uses the previous revision. |
| `consumer_resource_id` | Function App or worker restarted by `restart_consumer`. |
| `health_url` | HTTP probe. 2xx means the action held. |
| `fix_mode` | Optional override of `fix.mode` for this resource: `template`, `model_patch`, or `issue` (`copilot_issue` is an alias). Empty inherits the process mode. |
| `fix_agent` | Optional override of `fix.issue.agent` for this resource: `copilot`, `claude`, or `none`. |

## Rules

A rule matches when every set condition is true.

| Field | Meaning |
| --- | --- |
| `tier` | 1, 2, or 3. |
| `min_severity` | `sev0` through `sev4`. `sev2` matches sev2, sev1, and sev0. |
| `min_count` | Incident repeat count. Default 1. |
| `min_error_rate` | Requires `error_rate` on the signal, as a fraction. |
| `requires_recent_deploy` | A deployment or revision inside `deploy_window`. |
| `requires_likely_code` | The hypothesis marked the cause as code. |
| `categories` | Empty allows any category. |
| `when_tier2` | Tier 3 only. The tier-2 outcome must be in the list. |

Tier 1 diagnosis runs for a new incident, a worse severity, or a notify milestone even when you add no tier-1 rule. The tier-1 rule is what the shipped config uses as documentation of the severity floor. Notification fires on the counts in `notify_counts` and when severity gets worse.

## Shipped rules

`rollback-after-deploy` allows tier 2 for `code`, `deployment`, or `config` only when something shipped inside the window. The action selector then prefers a slot swap or a revision rollback.

`capacity-or-dependency` allows tier 2 without a deployment, and only `scale_out` for capacity or a restart for a dependency. It does not open a pull request.

`repeat-offender` allows tier 2 after five occurrences at sev3 or worse, still subject to the allowlist.

`code-pull-request` reaches tier 3 when the hypothesis says the cause is code and tier 2 ended in `dry_run`, `failed`, `not_hold`, `unverified`, `skipped`, `rate_limited`, `cooldown`, `breaker_open`, or `at_capacity`. What tier 3 opens depends on `fix.mode`: a template pull request, a model patch, or an issue. See [fix.md](fix.md).

## Action choice

The hypothesis can name a suggested action. Healbot uses it only when it is on the allowlist and legal for the resource type. Otherwise it walks a fixed order and keeps the first action the category allows:

- code, deployment, config: slot swap, then revision rollback
- capacity: scale out
- dependency: restart, revision restart, or consumer restart

If nothing matches, tier 2 is skipped and tier 3 still evaluates `when_tier2: [skipped, ...]`.

## Example

A production API that may roll back a slot, but only as a dry run until the health probe exists:

```yaml
resources:
  - id: /subscriptions/SUB/resourceGroups/prod/providers/Microsoft.Web/sites/orders
    max_tier: 3
    dry_run: true
    allowed_actions: [slot_swap, restart]
    known_good_slot: staging
    health_url: https://orders.example.internal/health
    max_actions_per_hour: 3
    cooldown: 15m
```

Set the process `dry_run` to false and this resource's `dry_run` to false together when you want a live swap. Leave `max_tier` at 2 if you do not want pull requests from this service.
