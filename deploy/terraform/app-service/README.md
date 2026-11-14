# App Service

Linux App Service custom container for Healbot. This directory is a Terraform root. Apply it from here, not from `deploy/terraform`.

`deploy/main.bicep` is the Container Apps-only template. The Container Apps Terraform root is [../container-apps](../container-apps/README.md). Role actions match [docs/rbac.md](../../../docs/rbac.md).

## What this creates

The [healbot-core](../modules/healbot-core/README.md) module creates the resource group, user-assigned identity, Healbot Operator role (one definition per operator scope), Key Vault tokens, Log Analytics, and Application Insights. This root then creates:

- a Linux plan, default SKU `B1`, which is the cheapest tier with Always On and custom containers
- a site with HTTPS only, TLS 1.2, health check `/healthz`, and `WEBSITES_PORT=43117`
- an action group webhook to `/v1/ingest/monitor`, a HTTP 5xx metric alert, and a resource-health alert

The process starts with `HEALBOT_DRY_RUN=true` and `/config.azure.yaml`, which has an empty `resources` list. Unknown resources stay at tier 1. `HEALBOT_FIX_*` is not set, so tier 3 stays `template`.

List price for B1 is on the order of the low tens of US dollars per month and varies by region. Log Analytics is pay-per-GB under a 1 GB daily cap. Key Vault and the activity-log alert are small or free. One metric alert is included. No registry unless you opt in. This stack was not applied, so those figures are list-price estimates, not a bill.

## Prerequisites

- Terraform 1.6 or newer. CI uses 1.13.3
- Azure CLI, logged in: `az login`
- Permission to create the resource group and to create role definitions and assignments on every operator scope. Owner, or Contributor plus User Access Administrator, is enough
- Docker, if you are building the image
- A registry the site can pull. A public registry needs no extra role. A private Azure Container Registry needs `create_container_registry` or `container_registry_id`

## Build and push

From the repository root:

```bash
docker build -t healbot:0.1.0 .
```

The image listens on 43117, runs as uid 65532, and can write `/var/lib/healbot/audit.jsonl`. `HEALBOT_CONFIG` selects `/config.azure.yaml` even though the image command still points at the demo file.

To let this stack create a Basic registry:

```bash
cd deploy/terraform/app-service
terraform apply -var='create_container_registry=true' -var='container_image=healbot:0.1.0'
LOGIN=$(terraform output -raw registry_login_server)
az acr login --name "${LOGIN%%.*}"
docker tag healbot:0.1.0 "$LOGIN/healbot:0.1.0"
docker push "$LOGIN/healbot:0.1.0"
```

The first site start can fail until that push exists. Apply again after the push. For a registry you already have, set `container_image` to the full reference and set `container_registry_id` when the site should pull with the managed identity.

## Configure

```bash
cd deploy/terraform/app-service
cp example.tfvars terraform.tfvars
```

`terraform.tfvars` is gitignored. `example.tfvars` has only placeholders. Edit the image, the region, and `target_resource_group_ids` if Healbot should touch other groups. Leave `dry_run = true`. Pass notifier tokens with `TF_VAR_notifier_tokens` or an untracked file. Do not commit them.

State contains the generated webhook and admin tokens and the action-group URL. Keep it out of git. The commented backend in `versions.tf` uses the placeholders `example-tfstate-rg` and `exampletfstate000`. Replace those only in a copy you do not commit, then `terraform init -migrate-state`.

## Init, plan, apply

```bash
az login
terraform init
terraform plan -var-file=example.tfvars
terraform apply -var-file=terraform.tfvars
```

The first apply waits about a minute after the Key Vault role assignments. If an assignment reports that the custom role does not exist yet, run apply again.

```bash
terraform output -raw webhook_token
terraform output -raw admin_token
az keyvault secret show --vault-name "$(terraform output -raw key_vault_name)" --name webhook-token --query value -o tsv
```

`terraform output` redacts sensitive values unless you pass `-raw`.

## Alerts

The stack attaches an HTTP 5xx metric alert (5 or more in 15 minutes) and a resource-health alert for `Degraded` or `Unavailable`. Add `terraform output -raw action_group_id` to any other alert. Use the common alert schema.

Action groups cannot set `X-Healbot-Token`. The token is in the query string, so it is stored on the action group and in state. This stack does not enable App Service HTTP logs. Prefer a header on any client that can set one.

## Verify

Azure mode refuses `POST /v1/demo/scenario`. Check the process, then post one common-alert payload.

```bash
HOST=$(terraform output -raw hostname)
curl -fsS "https://${HOST}/healthz"
curl -fsS "https://${HOST}/readyz"
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
      "firedDateTime": "2026-11-13T14:02:01.123Z",
      "description": "Failed requests spiked after the production slot swap."
    },
    "alertContext": {
      "properties": {
        "exceptionType": "System.NullReferenceException",
        "message": "Object reference not set to an instance of an object.",
        "operation": "POST /v1/orders",
        "topFrame": "OrderService.Place"
      }
    }
  }
}
JSON
```

With the defaults that resource is not allowlisted, so the incident is diagnosed and capped at tier 1. The console at `https://<hostname>/` does not include the admin token. Type it into a control form, or send `X-Healbot-Token`.

If `/healthz` is healthy but ingest returns 401, restart the site so Key Vault references resolve:

```bash
az webapp restart \
  -g "$(terraform output -raw resource_group_name)" \
  -n "$(terraform output -raw hostname | cut -d. -f1)"
```

The audit file on the container disk is not kept across a new container. `GET /v1/audit` still serves the memory ring.

## App Service targets

`restart_revision` and `rollback_revision` apply only to Container Apps. They do not roll back this site or any other App Service. The reversible rollback for an App Service target is `slot_swap`. Scale-out of a plan needs `enable_plan_scale = true`. An App Service target without `health_url` ends as `unverified` after a live call. To roll back Healbot itself, deploy the previous image. This root does not create a deployment slot.

## Destroy

```bash
terraform destroy -var-file=terraform.tfvars
```

Key Vault soft-delete keeps the name for 7 days. A later apply uses a new random suffix. Purge protection is off. Purge a deleted vault with `az keyvault purge` only if you need the name back. Secrets Officer does not include purge.
