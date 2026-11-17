env_name       = "gcp-sandbox"
state_location = "clanker-sandbox-gcp-sandbox-tfstate"

project_id = "clanker-sandbox"
region     = "us-central1"

kubernetes_version = "1.36"
node_image_version = ""
node_count         = 2
node_size          = "e2-standard-2"

# The sandbox has a TTL, so OpenTofu must be able to destroy the cluster.
deletion_protection = false
