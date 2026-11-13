# Copy to terraform.tfvars (gitignored) or pass with -var-file.
# Replace the image before apply. Do not put real tokens, subscription ids,
# tenant ids, or email addresses in a file you commit.

name     = "healbot"
location = "eastus"
hosting  = "app_service"

# Full image reference when create_container_registry is false.
# When create_container_registry is true, use repository:tag instead,
# for example "healbot:0.1.0".
container_image           = "example.azurecr.io/healbot:0.1.0"
create_container_registry = false
container_registry_id     = ""

app_service_sku = "B1"

# Ceiling. Live remediation also needs a resource entry in config with
# dry_run: false and an allowlist. See the README.
dry_run = true

enable_plan_scale      = false
enable_container_scale = false

# Resource groups the identity may remediate, besides its own.
# Use an untracked tfvars file for real ids. The zero GUID below is a shape example.
# target_resource_group_ids = [
#   "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg",
# ]
target_resource_group_ids = []

log_analytics_daily_quota_gb = 1
log_analytics_retention_days = 30

# Pass notifier tokens out of band, for example:
# export TF_VAR_notifier_tokens='{"GITHUB_TOKEN":"<from your secret store>"}'
notifier_tokens = {}

tags = {}
