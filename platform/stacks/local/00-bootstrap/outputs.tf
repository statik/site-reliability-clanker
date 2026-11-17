output "state_backend" {
  value       = { path = var.state_location }
  description = "Backend configuration for the other layers."
}

output "evidence_bucket" {
  value       = dirname(local_file.evidence_marker.filename)
  description = "Directory for audit evidence and backups."
}

output "identities" {
  value       = local.identities
  description = "Kubernetes service account names that the broker issues tokens for."
}

output "permission_guard" {
  value       = ""
  description = "The local target has no permission guard."
}
