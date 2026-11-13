# Safety model

Healbot treats a mutation as the exception. Every live action has to pass every gate below, in order. The demo passes the early gates and stops at dry run, which is why the simulator can show tier 2 without an Azure call.

## Gates

1. **Catalog.** The action must be one of `restart`, `scale_out`, `slot_swap`, `restart_revision`, `rollback_revision`, `restart_consumer`. There is no generic ARM call.
2. **Resource type.** Slot swap and process restart apply to `Microsoft.Web/sites` (App Service and Functions). Revision restart and rollback apply to `Microsoft.App/containerApps`.
3. **Allowlist.** The resource policy lists the actions that may run. An empty list means none.
4. **Tier cap.** `max_tier` defaults to 1 for any resource that is not in the config. Tier 2 requires at least 2. Tier 3 requires 3.
5. **Rule.** Escalation rules add conditions: severity, repeat count, error rate, category, a recent deployment, and for tier 3 the tier-2 outcome.
6. **Dry run.** `dry_run: true` on the process is a ceiling. A resource cannot turn it off. If the process flag is false, a resource still defaults to dry run unless it sets `dry_run: false`.
7. **Kill switch.** `POST /v1/control/kill` blocks tier 2 and tier 3, including dry-run plans. Tier 1 still records the incident. The change is audited as a human action.
8. **Suppress.** `POST /v1/incidents/{id}/suppress` stops automation for that incident.
9. **Rate, cooldown, breaker.** These apply to live calls only. Dry runs do not consume them, so a plan can still be shown while a breaker is open. After `breaker_failures` unsuccessful live attempts the resource is blocked for `breaker_open_for`. The next attempt is a single half-open trial.
10. **Health.** After a live call, Healbot probes `health_url` when it is set. Container Apps can also use the revision health state. App Service without `health_url` is `unverified` on purpose: the bot will not invent a public hostname. `unverified` and `not_hold` count as breaker failures and can escalate to tier 3.

## What a dry run records

The executor writes the concrete plan ("swap slot staging with production on example-api") and returns `dry_run`. It does not call `platform.Runtime`. The tier-3 rule treats `dry_run` as "the fix was not applied", so a code incident continues to a pull request. That is the path the simulator asserts.

## What tier 3 will not do

The pull-request body says Healbot does not merge. The GitHub and Azure DevOps clients expose `Create` only. Tests fail if a request path contains `merge`. The proposed files are a starting point for the service owner, generated from the stack frame, not a claim that the patch compiled in that repository.

## Queue consumers

`restart_consumer` restarts the resource in `consumer_resource_id`, or the incident resource when that is empty. It does not complete, abandon, or purge messages.

## Audit

Decision events are appended as JSONL when `audit.path` is set, and kept in a ring buffer for `GET /v1/audit`. Kinds include `incident.opened`, `diagnosis.completed`, `policy.decision`, `remediation.dry_run`, `remediation.blocked`, `verification.result`, `pr.opened`, `kill_switch.changed`, and `automation.blocked`.

## Network

In demo mode, ingest requires `HEALBOT_WEBHOOK_TOKEN` when it is set, and an empty token leaves ingest open for the local simulator. In azure mode an empty webhook token is rejected: ingest fails closed until `HEALBOT_WEBHOOK_TOKEN` is set, and an empty admin token rejects kill, resume, and suppress until `HEALBOT_ADMIN_TOKEN` is set. The literal `demo-token` is a demo-mode placeholder and is rejected in azure mode. Compare is constant-time. The console and the read APIs are unauthenticated so an operator can see state. Do not publish them on the internet. The Bicep ingress is there so an action group can reach the webhook. Restrict it with a network rule or a front door when you can, and prefer a header over the query-string token the action group has to use.
