postgres_version      = "17.5"
postgres_storage_gb   = 10
shared_fs_capacity_gb = 1024
backup_retention_days = 7

# The sandbox has a TTL, so OpenTofu must be able to destroy the database and the bucket.
deletion_protection = false
