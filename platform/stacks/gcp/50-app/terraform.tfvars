env_name       = "gcp-sandbox"
state_location = "clanker-sandbox-gcp-sandbox-tfstate"

project_id = "clanker-sandbox"
region     = "us-central1"

paperboat_version = "1.0.0"
api_replicas      = 2
worker_replicas   = 1
web_replicas      = 1
loadgen_enabled   = true
workload_runtime  = "kubernetes"
