# Container Apps

Azure Container Apps root for Healbot. Apply it from this directory, not from `deploy/terraform`.

The App Service root is [../app-service](../app-service/README.md). `deploy/main.bicep` is a separate Container Apps template that uses a system-assigned identity and does not create Key Vault, Log Analytics, or Application Insights. This root does. Role actions match [docs/rbac.md](../../../docs/rbac.md).

## What this creates

The [healbot-core](../modules/healbot-core/README.md) module creates the resource group, user-assigned identity, Healbot Operator role (one definition per operator scope), Key Vault tokens, Log Analytics, and Application Insights. This root then creates:

- a consumption environment with logs in that workspace
- a container app on port 43117, minimum 1 replica, maximum 2, probes on `/healthz` and `/readyz`
- an action group webhook to `/v1/ingest/monitor`, a restart-count metric alert, and a resource-health alert

The process starts with `HEALBOT_DRY_RUN=true` and `/config.azure.yaml`, which has an empty `resources` list. Unknown resources stay at tier 1. `HEALBOT_FIX_*` is not set, so tier 3 stays `template`.

A replica that stays at 0.5 CPU and 1 GiB usually costs more than App Service Basic B1. Log Analytics is pay-per-GB under a 1 GB daily cap. This stack was not applied, so that comparison is a list-price estimate, not a bill.

## Prerequisites

- Terraform 1.6 or newer. CI uses 1.13.3
- Azure CLI, logged in: `az login`
- Permission to create the resource group and to create role definitions and assignments on every operator scope. Owner, or Contributor plus User Access Administrator, is enough
- Docker, if you are building the image
- A registry the app can pull. A public registry needs no extra role. A private Azure Container Registry needs `create_container_registry` or `container_registry_id`

## Build and push

From the repository root:

```bash
docker build -t healbot:0.1.0 .
```

The image listens on 43117, runs as uid 65532, and can write `/var/lib/healbot/audit.jsonl`. `HEALBOT_CONFIG` selects `/config.azure.yaml`.

To let this stack create a Basic registry:

```bash
cd deploy/terraform/container-apps
terraform apply -var='create_container_registry=true' -var='container_image=healbot:0.1.0'
LOGIN=$(terraform output -raw registry_login_server)
az acr login --name "${LOGIN%%.*}"
docker tag healbot:0.1.0 "$LOGIN/healbot:0.1.0"
docker push "$LOGIN/healbot:0.1.0"
```

The first revision can fail until that push exists. Apply again after the push. For a registry you already have, set `container_image` to the full reference and set `container_registry_id` when the app should pull with the managed identity.

## Configure

```bash
cd deploy/terraform/container-apps
cp example.tfvars terraform.tfvars
```

`terraform.tfvars` is gitignored. `example.tfvars` has only placeholders. Edit the image, the region, and `target_resource_group_ids` if Healbot should touch other groups. Leave `dry_run = true`. Pass notifier tokens with `TF_VAR_notifier_tokens` or an untracked file. Do not commit them.

State contains the generated webhook and admin tokens and the action-group URL. Keep it out of git. The commented backend in `versions.tf` uses the placeholders `example-tfstate-rg` and `exampletfstate000`, with a state key distinct from the App Service root. Replace those only in a copy you do not commit, then `terraform init -migrate-state`.

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

## Alerts

The stack attaches a restart-count metric alert (3 or more in 15 minutes) and a resource-health alert for `Degraded` or `Unavailable`. Add `terraform output -raw action_group_id` to any other alert. Use the common alert schema.

Action groups cannot set `X-Healbot-Token`. The token is in the query string, so it is stored on the action group and in state. Prefer a header on any client that can set one.

## Verify

Azure mode refuses `POST /v1/demo/scenario`. The same common-alert POST as the [App Service README](../app-service/README.md#verify) applies here. `GET /healthz` and `GET /readyz` return `{"status":"ok"}`. With the defaults the sample resource is diagnosed and capped at tier 1.

If health is fine but ingest returns 401, restart the active revision:

```bash
APP=$(terraform output -raw hostname | cut -d. -f1)
RG=$(terraform output -raw resource_group_name)
REV=$(az containerapp show -g "$RG" -n "$APP" --query properties.latestRevisionName -o tsv)
az containerapp revision restart -g "$RG" -n "$APP" --revision "$REV"
```

The audit file on the container disk is not kept across a new revision. `GET /v1/audit` still serves the memory ring.

## Revisions

This host is itself a Container App, so `restart_revision` and `rollback_revision` can target other Container Apps when the role allows it. Activating a revision does not need `enable_container_scale`. Changing scale or ingress traffic weights does. App Service targets still roll back with `slot_swap`, and scale-out of a plan needs `enable_plan_scale`. An App Service target without `health_url` ends as `unverified`.

## Destroy

```bash
terraform destroy -var-file=terraform.tfvars
```

Key Vault soft-delete keeps the name for 7 days. A later apply uses a new random suffix. Purge protection is off.
