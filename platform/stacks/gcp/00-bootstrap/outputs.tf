output "state_backend" {
  description = "Backend configuration for the other layers."
  value = {
    bucket = module.state_bucket.name
  }
}

output "evidence_bucket" {
  description = "Bucket for audit evidence and backups."
  value       = module.evidence_bucket.name
}

output "identities" {
  description = "Service account email of each agent identity."
  value       = module.agent_identities.identities
}

output "permission_guard" {
  description = "ID of the IAM deny policy that covers the agent identities."
  value       = module.agent_identities.permission_guard
}

output "clanker_ttl" {
  description = "Value of the clanker_ttl label. The other layers read it."
  value       = local.clanker_ttl
}

output "node_service_account" {
  description = "Email of the service account for GKE nodes."
  value       = module.agent_identities.node_service_account
}
