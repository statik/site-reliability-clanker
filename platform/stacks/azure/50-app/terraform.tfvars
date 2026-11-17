env_name        = "azure-sandbox"
state_location  = "clankerazuresandbox/tfstate"
subscription_id = "00000000-0000-0000-0000-000000000000"
location        = "westus3"

paperboat_version = "1.0.0"
api_replicas      = 2
worker_replicas   = 1
web_replicas      = 2
loadgen_enabled   = true
workload_runtime  = "kubernetes"
