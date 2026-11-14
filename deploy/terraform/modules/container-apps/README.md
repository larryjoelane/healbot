# container-apps

Consumption Container Apps environment and a single Healbot container. Ingress is external on port 43117, with probes on `/healthz` and `/readyz`, and at least one replica so the webhook stays up.

Call `healbot-core` first. Secrets are Key Vault references resolved with the user-assigned identity.
