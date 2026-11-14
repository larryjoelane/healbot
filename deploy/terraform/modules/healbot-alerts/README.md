# healbot-alerts

Action group and the two sandbox alerts. Both hosting roots call this after the host exists, because the webhook URL needs that hostname.

Action groups cannot set `X-Healbot-Token`. The ingest token is in the query string, so it is stored on the action group and in Terraform state. Do not enable HTTP request logs that record query strings.
