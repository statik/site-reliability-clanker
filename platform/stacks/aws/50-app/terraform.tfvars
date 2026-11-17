env_name       = "aws-sandbox"
state_location = "clanker-aws-sandbox-state-000000000000"
region         = "us-east-1"

paperboat_version = "1.0.0"
api_replicas      = 2
worker_replicas   = 1
web_replicas      = 2
loadgen_enabled   = true
workload_runtime  = "kubernetes"
