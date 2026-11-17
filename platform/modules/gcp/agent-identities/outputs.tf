output "identities" {
  description = "Service account email of each agent identity."
  value       = { for name, account in google_service_account.agent : name => account.email }
}

output "permission_guard" {
  description = "ID of the IAM deny policy that covers the agent identities."
  value       = google_iam_deny_policy.guard.id
}

output "node_service_account" {
  description = "Email of the service account for GKE nodes."
  value       = google_service_account.node.email
}
