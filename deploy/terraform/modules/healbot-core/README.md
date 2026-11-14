# healbot-core

Shared resources for a Healbot sandbox. Both hosting roots call this module. It does not create the App Service site or the Container App, because a deployment uses one of those and not both.

It creates:

- the resource group, unless a name is passed in
- a user-assigned managed identity
- a Healbot Operator custom role on each operator scope
- Key Vault, with generated webhook and admin tokens
- a Log Analytics workspace and Application Insights
- an optional Basic container registry and `AcrPull`

The action group is a separate module, `healbot-alerts`, because its webhook URL needs the hostname of whichever host is deployed.
