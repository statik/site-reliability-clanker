output "state_backend" {
  description = "Backend configuration for the other layers. Add key = <env_name>/<layer>.tfstate."
  value = {
    bucket       = module.state_bucket.name
    region       = var.region
    encrypt      = "true"
    use_lockfile = "true"
  }
}

output "evidence_bucket" {
  description = "Bucket for audit evidence and backups."
  value       = module.evidence_bucket.name
}

output "identities" {
  description = "Role ARN for each agent identity."
  value       = module.agent_identities.role_arns
}

output "permission_guard" {
  description = "ARN of the permission boundary on each agent role."
  value       = module.agent_identities.boundary_arn
}

output "clanker_ttl" {
  description = "Value of the clanker_ttl tag. The other layers copy it."
  value       = local.clanker_ttl
}

output "workload_role_path" {
  description = "IAM path under which 20-cluster creates the cluster, node and pod roles."
  value       = module.agent_identities.workload_role_path
}
