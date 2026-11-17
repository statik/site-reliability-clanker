env_name         = "gcp-sandbox"
state_location   = "clanker-sandbox-gcp-sandbox-tfstate"
broker_principal = "serviceAccount:clanker-broker@clanker-sandbox.iam.gserviceaccount.com"
ttl_hours        = 72

project_id = "clanker-sandbox"
region     = "us-central1"

# No grant window is open. The broker cannot use an applier or the debugger until a
# window is added here and this layer is applied.
grant_windows = {}
