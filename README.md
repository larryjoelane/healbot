# Healbot

Healbot watches Azure applications for failures, explains them, and responds through three tiers. Tier 1 diagnoses. Tier 2 may plan a reversible runbook. Tier 3 opens a pull request for a person to review. It never merges that pull request.

The default mode is a dry run. You can watch a full incident on a laptop with no Azure subscription.

## Why Go

The hot path is a stream of log lines and alert payloads. Go is a good fit for that work:

- The parser and fingerprint run with zero allocations per line after the buffer pool is warm (`BenchmarkParseLogLine`, `BenchmarkFingerprint`).
- Bounded worker pools block the reader when diagnosis falls behind, so a burst cannot grow memory without limit.
- One static binary talks to Azure through the official SDK and `DefaultAzureCredential`. Managed identity is the production credential. There is no client secret in the source.
- `go test -race` covers the sharded grouper and the incident store.

## Quickstart

```bash
go run ./cmd/simulator
```

That starts the bot, posts one Azure Monitor common-alert payload, and prints the trail:

1. Tier 1: `System.NullReferenceException` in `OrderService.Place` after a slot swap twelve minutes ago.
2. Tier 2: a dry-run plan to swap the `staging` slot back. The fake Azure runtime records no call.
3. Tier 3: a pull request on the in-memory repository, including a regression test. Nothing is merged.

Run the service on its own and open the console:

```bash
go run ./cmd/healbot -config config.demo.yaml
```

[http://127.0.0.1:43117](http://127.0.0.1:43117) has the same scenario button. `GET /healthz` returns `{"status":"ok"}`. The ingest token for direct curls against the demo config is `demo-token`, sent as `X-Healbot-Token`. That value is a demo-mode placeholder. Azure mode starts with an empty token unless `HEALBOT_WEBHOOK_TOKEN` is set, and then rejects ingest.

## What the tiers do

| Tier | Does | Does not |
| --- | --- | --- |
| 1 Detect | Group signals, gather deployments, write a hypothesis, notify | Change Azure or git |
| 2 Remediate | Restart, scale out, swap a slot, roll a container revision, restart a queue consumer | Delete resources, edit app settings, purge queues, run shell |
| 3 Code fix | Open a GitHub or Azure DevOps pull request with a patch and a test | Merge, approve, or complete the pull request |

Unknown resources are capped at tier 1. Global `dry_run: true` cannot be overridden by a resource. See [docs/safety.md](docs/safety.md) and [docs/policy.md](docs/policy.md).

## Configuration

`config.demo.yaml` is the local file. `config.azure.example.yaml` is the shape for a subscription. Environment variables override the file:

| Variable | Effect |
| --- | --- |
| `HEALBOT_ADDR` | Listen address |
| `HEALBOT_MODE` | `demo` or `azure` |
| `HEALBOT_DRY_RUN` | `true` forces dry run |
| `HEALBOT_WEBHOOK_TOKEN` | Required on ingest when set. Azure mode rejects ingest when this is empty |
| `HEALBOT_ADMIN_TOKEN` | Required on kill, resume, suppress, and the demo button when set. Azure mode rejects those actions when this is empty |
| `HEALBOT_SUBSCRIPTION_ID` | Subscription for the Azure SDK |
| `HEALBOT_WORKSPACE_ID` | Log Analytics workspace GUID |
| `HEALBOT_AUDIT_PATH` | JSONL audit file. Empty keeps it in memory |
| `AZURE_OPENAI_ENDPOINT` / `AZURE_OPENAI_DEPLOYMENT` | Optional model. The heuristic remains the fallback |
| `GITHUB_TOKEN` / `AZDO_TOKEN` | Only when `repo.provider` is not `mock` |

`mode: azure` uses `azidentity.NewDefaultAzureCredential`. In Azure that is the container app's system-assigned managed identity. Locally it is the Azure CLI session. Healbot does not read a client secret from code.

## Ingest

- `POST /v1/ingest/monitor` accepts the [Azure Monitor common alert schema](https://learn.microsoft.com/azure/azure-monitor/alerts/alerts-common-schema). Add `?sync=1` to wait for the decision. Otherwise the handler returns 202, or 429 when the diagnose queue is full.
- `POST /v1/ingest/logs` accepts NDJSON or the pipe format below. The scanner stops reading when the worker queue is full.
- An optional poller runs the KQL in `poller.kql` against the workspace.

Pipe format, one error per line:

```text
2026-11-13T14:02:01.123Z|2|/subscriptions/.../sites/example-api|System.NullReferenceException|POST /v1/orders|OrderService.Place|Object reference not set
```

Digits in the message collapse, so "failed 12 times" and "failed 99 times" share a fingerprint. Repeats update the open incident instead of starting a new one.

## HTTP API

| Method | Path | Auth |
| --- | --- | --- |
| GET | `/healthz`, `/readyz` | none |
| POST | `/v1/ingest/monitor`, `/v1/ingest/logs` | webhook token |
| GET | `/v1/incidents`, `/v1/incidents/{id}`, `/v1/audit`, `/v1/control/status` | none (put the console on a private network) |
| POST | `/v1/control/kill`, `/v1/control/resume`, `/v1/incidents/{id}/suppress` | admin token |
| POST | `/v1/demo/scenario` | admin token, demo mode only |
| GET | `/`, `/incidents/{id}`, `/audit`, `/pulls/{id}` | console |

The kill switch still records and diagnoses. It refuses tier 2 and tier 3 until `resume`.

## Deploy on Azure

Build the image from the Dockerfile and push it to a registry the container app can pull. Then deploy the Bicep in `deploy/main.bicep`:

```bash
az group create -n healbot-rg -l eastus
az deployment group create \
  -g healbot-rg \
  -f deploy/main.bicep \
  -p containerImage=REGISTRY/healbot:0.1.0 \
  -p webhookToken="$HEALBOT_WEBHOOK_TOKEN"
```

The template creates:

- a Container Apps environment and a container app with a system-assigned identity
- a custom role, Healbot Operator, and an assignment to that identity
- an action group whose webhook posts the common alert schema to `/v1/ingest/monitor`

The app starts in dry run. Attach an existing alert to the action group. Flip `HEALBOT_DRY_RUN` to `false` and set a resource's `dry_run: false` only after `health_url` is set. Role details are in [docs/rbac.md](docs/rbac.md).

## Develop

```bash
go test ./...
go test -race ./...
go test -bench=. -benchtime=50ms -run='^$' ./internal/ingest ./internal/group ./internal/store
```

CI runs the same steps plus `golangci-lint` and a gitleaks secret scan. The Azure SDK calls are tested against an in-process HTTP transport, not a live subscription.

## Layout

```text
cmd/healbot        service
cmd/simulator      one-shot demo
internal/ingest    alert and log parsers, bounded pool
internal/group     fingerprint and sharded counter
internal/pipeline  tier loop
internal/remediate runbooks
internal/fix       pull requests (mock, GitHub, Azure DevOps)
internal/azure     DefaultAzureCredential adapters
deploy/main.bicep  the bot, the role, the action group
```

## Implemented and intentionally absent

Implemented: common-alert and log ingest, dedupe, heuristic and optional Azure OpenAI diagnosis, policy, kill switch, rate limit, cooldown, circuit breaker, dry-run by default, the five reversible runbooks, health verification, GitHub and Azure DevOps pull requests, Teams/Slack/JSON webhooks, SMTP, GitHub issues, JSONL audit, the console, Bicep, and benchmarks.

Not implemented, on purpose:

- Merging or auto-completing a pull request. The clients have no merge method.
- Purging or dead-lettering a queue. `restart_consumer` restarts the Function App or worker and leaves the messages alone.
- Arbitrary ARM writes. The runtime exposes only the runbook methods.
- A claim that a live action succeeded when health cannot be checked. Missing `health_url` on App Service ends as `unverified` and escalates.

The heuristic is the default diagnosis. Azure OpenAI is used only when `llm.provider` is `azure_openai` and the endpoint is set. A model failure falls back to the heuristic and is written to the audit log.
