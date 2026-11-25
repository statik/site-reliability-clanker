kubernetes_version = "1.36"
node_image_version = ""
node_count         = 2
node_size          = "e2-standard-2"

# The sandbox has a TTL, so OpenTofu must be able to destroy the cluster.
deletion_protection = false
