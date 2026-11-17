output "app_url" {
  description = "URL that serves Paperboat."
  value       = module.app.app_url
}

output "paperboat_version" {
  description = "Release that runs now."
  value       = module.app.paperboat_version
}

output "release_revision" {
  description = "Helm revision."
  value       = module.app.release_revision
}
