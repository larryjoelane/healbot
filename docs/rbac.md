# Least-privilege RBAC

Healbot's identity is a managed identity. `deploy/main.bicep` uses the system-assigned identity on the container app. The Terraform roots `deploy/terraform/app-service` and `deploy/terraform/container-apps` use a user-assigned identity so Key Vault and registry grants exist before the app starts, and set `AZURE_CLIENT_ID` so `DefaultAzureCredential` selects it. Each creates a custom role, **Healbot Operator**, and assigns it to that identity.

The Bicep role is defined on the bot's resource group. Each Terraform root defines one role per operator scope: the bot's group, plus any `target_resource_group_ids`. Each definition's only assignable scope is that group, so the role cannot be granted across the subscription. The actions are the same list below.

The role is the first gate. The resource allowlist in config is the second. A permission the role has is still unused until a resource lists that action and dry run is off.

## Included actions

| Permission | Why |
| --- | --- |
| `Microsoft.Web/sites/read` | Read the site and its plan id |
| `Microsoft.Web/sites/restart/action` | Restart an App Service or Function App |
| `Microsoft.Web/sites/slots/read` | See deployment slots |
| `Microsoft.Web/sites/slots/restart/action` | Restart a slot |
| `Microsoft.Web/sites/slots/slotsswap/action` | Swap back to the known-good slot |
| `Microsoft.Web/sites/deployments/read` | Recent deployments for the diagnosis |
| `Microsoft.Web/sites/config/read` | Read configuration. Not write |
| `Microsoft.Web/serverfarms/read` | Read the plan before a scale decision |
| `Microsoft.App/containerApps/read` | Read the container app |
| `Microsoft.App/containerApps/revisions/read` | List revisions |
| `Microsoft.App/containerApps/revisions/restart/action` | Restart a revision |
| `Microsoft.App/containerApps/revisions/activate/action` | Activate a known-good revision |
| `Microsoft.Insights/components/read` | Read an Application Insights component |
| `Microsoft.OperationalInsights/workspaces/read` | See the workspace |
| `Microsoft.OperationalInsights/workspaces/query/action` | Run the optional KQL poller |

## Explicitly omitted

| Permission | Why it stays off |
| --- | --- |
| `Microsoft.Web/sites/write` | Would allow changing app settings and the site itself |
| `Microsoft.Web/sites/delete` | Not a runbook |
| `Microsoft.App/containerApps/delete` | Not a runbook |
| `Microsoft.Web/serverfarms/write` | Scale-out of a plan. Opt in with `enablePlanScale=true` |
| `Microsoft.App/containerApps/write` | Scale and traffic-weight updates. Opt in with `enableContainerScale=true` |

`notActions` repeats the delete and site-write denials inside the role so a later broad assignment does not sneak them in through this definition.

## Opt-in scale

App Service scale changes the plan SKU capacity. Azure models that as `Microsoft.Web/serverfarms/write`, which can also resize the SKU. Healbot only writes `sku.capacity`, but the control plane permission is wider than the code. Leave `enablePlanScale` false unless that resource's allowlist contains `scale_out` and `max_instances` is set.

Container App scale and a traffic shift back to an older revision are JSON merge patches on the container app, which require `Microsoft.App/containerApps/write`. Activating a revision does not. Leave `enableContainerScale` false when rollback only needs `activate`.

## Workspace in another resource group

The custom role's assignable scope is the bot's resource group. If the Log Analytics workspace lives elsewhere, assign **Log Analytics Reader** (`Microsoft.OperationalInsights/workspaces/query/action` is included in that built-in role) on the workspace or its resource group. Do not widen the bot's role to the subscription to solve that.

## What the bot does not receive

No Key Vault secrets are required for Azure data-plane access. The webhook token is injected as `HEALBOT_WEBHOOK_TOKEN`. The Bicep template passes it as a container app secret. Terraform generates it, stores it in Key Vault, and references that secret from the app. Azure OpenAI, when enabled, uses the same managed identity (`https://cognitiveservices.azure.com/.default`) unless `AZURE_OPENAI_API_KEY` is set. Prefer the identity. The OpenAI resource still needs a **Cognitive Services OpenAI User** assignment, which neither deployment grants, because the account may be in another group.

GitHub and Azure DevOps tokens are git credentials, not Azure RBAC. Pass them as container app secrets. The demo uses the mock provider and needs neither.
