# app-service

Linux App Service plan and custom container for Healbot. The plan defaults are chosen by the calling root. This module sets Always On, HTTPS only, TLS 1.2, health check `/healthz`, and `WEBSITES_PORT=43117`.

Call `healbot-core` first. The identity and Key Vault references must exist before this site is created.
