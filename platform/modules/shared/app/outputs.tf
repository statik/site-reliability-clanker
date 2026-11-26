output "url" {
  value       = local.url
  description = "URL that serves the application."
}

output "version" {
  value       = var.app.version
  description = "Release of the application that runs now."
}

output "release_revision" {
  value       = helm_release.app.metadata.revision
  description = "Helm revision of the main release."
}
