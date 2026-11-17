output "app_url" {
  value       = "http://${var.public_address}"
  description = "URL that serves Paperboat."
}

output "paperboat_version" {
  value       = var.paperboat_version
  description = "Paperboat release that runs now."
}

output "release_revision" {
  value       = helm_release.paperboat.metadata.revision
  description = "Helm revision."
}
