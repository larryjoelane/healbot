# Terraform

Pick one root. A deployment runs Healbot on App Service or on Container Apps, not both.

| Root | Use it when |
| --- | --- |
| [app-service](app-service/README.md) | Linux App Service, custom container. This is the sandbox default. Basic B1, Always On, port 43117. |
| [container-apps](container-apps/README.md) | Azure Container Apps. Same role and dry-run ceiling as the Bicep file in `deploy/main.bicep`. |

Shared modules:

- [modules/healbot-core](modules/healbot-core/README.md) creates the resource group, user-assigned identity, Healbot Operator role, Key Vault, Log Analytics, and Application Insights.
- [modules/healbot-alerts](modules/healbot-alerts/README.md) creates the action group and the sandbox alerts. It is separate because the webhook URL needs the hostname of the host you picked.
- [modules/app-service](modules/app-service/README.md) and [modules/container-apps](modules/container-apps/README.md) are the hosting resources. The roots call them. Do not apply a module directory on its own.

Both roots start Healbot with `dry_run` true and the image config's empty resource list, so unknown resources stay at tier 1. They do not set `HEALBOT_FIX_*`. Unset, those stay at `template`. See [docs/fix.md](../../docs/fix.md).

If you applied the previous single root at `deploy/terraform` (the one with a `hosting` variable), destroy that state before applying a new root. The new roots do not adopt that state.

Remote state is a commented example in each root's `versions.tf`. The placeholder names are `example-tfstate-rg` and `exampletfstate000`. Do not commit a backend that points at a real account.
