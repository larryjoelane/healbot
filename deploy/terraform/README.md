# Deploy Healbot on Azure

This stack runs Healbot in a sandbox subscription. The default host is a Linux App Service plan with a custom container. Set `hosting = "container_apps"` to use Azure Container Apps instead. `deploy/main.bicep` is the older Container Apps-only template. The role actions match that file and [docs/rbac.md](../../docs/rbac.md).

Nothing here is applied against a live subscription by CI. `terraform validate` checks the configuration. You apply it.

## What it creates

- A resource group, unless you set `resource_group_name`
- A user-assigned managed identity, and `AZURE_CLIENT_ID` so `DefaultAzureCredential` selects it
- A custom role, Healbot Operator, once per operator scope. The bot's resource group is always a scope. `target_resource_group_ids` adds more. Each role can be assigned only on that group
- Key Vault secrets for the webhook token, the admin token, and any `notifier_tokens`. The app reads them as Key Vault references. Values are generated with `random_password` or passed in at apply time. They are not in git
- A Log Analytics workspace (PerGB2018, 30-day retention, 1 GB daily cap) and a workspace-based Application Insights component
- An action group whose webhook posts the common alert schema to `/v1/ingest/monitor`
- A metric alert and a resource-health activity log alert, both wired to that action group
- Either:
  - **app_service** (default): Linux plan, default SKU `B1`, Always On, HTTPS only, health check `/healthz`, `WEBSITES_PORT=43117`
  - **container_apps**: a consumption environment and a container app on port 43117, minimum 1 replica, probes on `/healthz` and `/readyz`

Optional: a Basic registry when `create_container_registry` is true. The identity is granted `AcrPull` before the app is created.

The process starts with `HEALBOT_DRY_RUN=true` and the image's `/config.azure.yaml`, which has an empty `resources` list. Unknown resources stay at tier 1. Live remediation is two deliberate changes: `dry_run = false` here, and a resource entry in the config with `dry_run: false` and an allowlist. See [docs/safety.md](../../docs/safety.md).

## Cost of the defaults

The default host is App Service **Basic B1**, the cheapest Linux tier that supports Always On and a custom container. List price is on the order of the low tens of US dollars per month and varies by region. Log Analytics is pay-per-GB with a 1 GB daily cap, so a quiet sandbox adds little. Key Vault, the action group webhook, and the activity-log alert are small or free. The metric alert is a single rule. No registry is created unless you opt in. Basic ACR is a few dollars per month. Container Apps with `min_replicas = 1` at 0.5 CPU and 1 GiB stays running and usually costs more than B1. These are list-price estimates, not a quote. This repository was not applied, so the numbers were not read from a bill.

## Prerequisites

- Terraform 1.6 or newer. CI uses 1.13.3
- Azure CLI, logged in to the sandbox subscription: `az login`
- Permission to create the resource group and to create role definitions and assignments on every operator scope. Owner, or Contributor plus User Access Administrator, is enough
- Docker, if you are building the image
- A registry the app can pull. A public registry needs no extra role. A private Azure Container Registry needs `create_container_registry` or `container_registry_id`

The identity is user-assigned on purpose. Grants for Key Vault and the registry are created before the app, which avoids the first-boot failure you get when a system-assigned identity does not exist until the app does.

## Build and push the image

From the repository root:

```bash
docker build -t healbot:0.1.0 .
```

The image listens on 43117, runs as uid 65532, and can write `/var/lib/healbot/audit.jsonl`. `HEALBOT_CONFIG` selects `/config.azure.yaml` even though the image command still points at the demo file. That file has `mode: azure`, `dry_run: true`, and no resources.

Push it somewhere the app can pull. For a registry this stack creates:

```bash
cd deploy/terraform
terraform apply -var='create_container_registry=true' -var='container_image=healbot:0.1.0'
# The app will not pull until the image exists. Push, then apply again if the first revision failed.
LOGIN=$(terraform output -raw registry_login_server)
az acr login --name "${LOGIN%%.*}"
docker tag healbot:0.1.0 "$LOGIN/healbot:0.1.0"
docker push "$LOGIN/healbot:0.1.0"
```

For a registry you already have, set `container_image` to the full reference, for example `example.azurecr.io/healbot:0.1.0`, and set `container_registry_id` when that registry is an Azure Container Registry that should be pulled with the managed identity.

## Configure

```bash
cd deploy/terraform
cp example.tfvars terraform.tfvars
```

`terraform.tfvars` is gitignored. `example.tfvars` has only placeholders. Edit:

- `container_image`
- `location`
- `target_resource_group_ids` if Healbot should touch groups other than its own. Use real ids only in the untracked file
- `notifier_tokens` for `GITHUB_TOKEN` or `AZDO_TOKEN`. Prefer `TF_VAR_notifier_tokens` in the environment of the apply if you do not want the values in a local file. They land in Terraform state either way
- leave `dry_run = true`

Do not commit `terraform.tfvars`, state, or a plan file.

## Remote state

State contains the generated tokens and the action-group URL. Keep it out of git. Local state is the default. For a remote backend, create a storage account outside this stack and uncomment the `backend "azurerm"` example in `versions.tf`. The commented values are placeholders (`example-tfstate-rg`, `exampletfstate000`). Replace them locally. Do not commit a backend block that names a real account. After editing it:

```bash
terraform init -migrate-state
```

CI runs `terraform init -backend=false`, so the commented backend is not required.

## Init, plan, apply

```bash
az login
terraform init
terraform plan -var-file=example.tfvars
terraform apply -var-file=terraform.tfvars
```

The first apply waits about a minute after the Key Vault role assignments so the secrets can be written. If an assignment reports that the custom role does not exist yet, run apply again. Role definitions take a moment to show up.

Then read the generated secrets from Key Vault, or from state:

```bash
terraform output -raw webhook_token
terraform output -raw admin_token
az keyvault secret show --vault-name "$(terraform output -raw key_vault_name)" --name webhook-token --query value -o tsv
```

`terraform output` redacts sensitive values unless you pass `-raw`. State holds them. Do not print them into tickets or commit the output.

## Wire alerts

The stack already attaches two alerts to the action group:

- A metric alert. On App Service it fires when HTTP 5xx responses in 15 minutes reach 5. On Container Apps it fires when restarts in 15 minutes reach 3
- An activity-log alert for resource health `Degraded` or `Unavailable` in the Healbot resource group

To send an existing alert as well, add the action group id from `terraform output action_group_id` to that alert's actions. Use the common alert schema. The webhook URL is `https://<hostname>/v1/ingest/monitor?token=<webhook token>`.

Action groups cannot set `X-Healbot-Token`. The token is in the query string. That value is stored on the action group, in Key Vault, and in Terraform state. It can also show up in any HTTP log that records query strings. This stack does not enable App Service HTTP logging for that reason. Prefer a private path or a front door in front of the site when you can. Healbot also accepts the header on any client that can set one.

## Verify

Azure mode refuses `POST /v1/demo/scenario`. The button on the local demo is not available here. Check the process, then post one common-alert payload, which is the same incident the simulator uses.

```bash
HOST=$(terraform output -raw hostname)
curl -fsS "https://${HOST}/healthz"
curl -fsS "https://${HOST}/readyz"
```

Both return `{"status":"ok"}`.

```bash
TOKEN=$(terraform output -raw webhook_token)
curl -fsS -X POST "https://${HOST}/v1/ingest/monitor?sync=1" \
  -H "content-type: application/json" \
  -H "X-Healbot-Token: ${TOKEN}" \
  -d @- <<'JSON'
{
  "schemaId": "azureMonitorCommonAlertSchema",
  "data": {
    "essentials": {
      "alertId": "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.AlertsManagement/alerts/example-api-orders",
      "alertRule": "example-api failed requests",
      "severity": "Sev2",
      "signalType": "Log",
      "monitorCondition": "Fired",
      "monitoringService": "Log Alerts V2",
      "alertTargetIDs": ["/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api"],
      "originAlertId": "example-api-orders",
      "firedDateTime": "2026-11-13T14:02:01.123Z",
      "description": "Failed requests spiked after the production slot swap."
    },
    "alertContext": {
      "properties": {
        "exceptionType": "System.NullReferenceException",
        "message": "Object reference not set to an instance of an object.",
        "operation": "POST /v1/orders",
        "topFrame": "OrderService.Place",
        "stack": "at Example.Orders.OrderService.Place(OrderService.cs:88)"
      }
    }
  }
}
JSON
```

With the defaults, that resource is not allowlisted, so the incident is diagnosed and capped at tier 1. Nothing is restarted or swapped. Open `https://<hostname>/` for the console and `https://<hostname>/v1/audit` for the decision log. The console does not include the admin token. Type it into a control form, or call kill and resume with `X-Healbot-Token` set to the admin token.

If `/healthz` is healthy but ingest returns 401, the Key Vault references may not have resolved on the first start. Restart and try again. On App Service the site name is the first label of the hostname:

```bash
az webapp restart \
  -g "$(terraform output -raw resource_group_name)" \
  -n "$(terraform output -raw hostname | cut -d. -f1)"
```

On Container Apps, restart the active revision:

```bash
az containerapp revision restart \
  -g "$(terraform output -raw resource_group_name)" \
  -n "$(terraform output -raw hostname | cut -d. -f1)" \
  --revision "$(az containerapp show -g "$(terraform output -raw resource_group_name)" -n "$(terraform output -raw hostname | cut -d. -f1)" --query properties.latestRevisionName -o tsv)"
```

The audit file is `/var/lib/healbot/audit.jsonl` inside the container. App Service does not persist that disk across a new container. The memory ring still answers `/v1/audit`.

## App Service targets and revision runbooks

`restart_revision` and `rollback_revision` apply only to `Microsoft.App/containerApps`. They do not roll back an App Service, including this bot when `hosting` is `app_service`. For an App Service target, the reversible rollback is `slot_swap` to `known_good_slot`. Scale-out of a plan needs `enable_plan_scale = true`, and scale or traffic-weight changes on a Container App need `enable_container_scale = true`. Leave both false until a resource allowlists that action. An App Service target without `health_url` ends as `unverified` after a live call. That is intentional.

To roll back Healbot itself on App Service, deploy the previous image. This stack does not create a deployment slot.

## Turn on live remediation

1. Set `health_url` on the resource you are willing to change
2. Add that resource to `/config.azure.yaml` with `max_tier`, `allowed_actions`, and `dry_run: false`, then rebuild and push the image
3. Set `dry_run = false` and apply

Either flag left on dry run keeps the action as a plan. The custom role still does not grant site delete or site write.

## Destroy

```bash
terraform destroy -var-file=terraform.tfvars
```

Key Vault soft-delete keeps the name for 7 days. A later apply uses a new random suffix, so the old name is not required. Purge protection is off so a sandbox can be removed. If destroy leaves a soft-deleted vault and you want the name back:

```bash
az keyvault purge --name "<vault-name>"
```

That needs permission to purge deleted vaults. The Key Vault Secrets Officer role used here does not include it.
