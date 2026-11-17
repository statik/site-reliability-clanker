output "app_url" {
  value       = module.app.app_url
  description = "URL that serves Paperboat."
}

output "paperboat_version" {
  value       = module.app.paperboat_version
  description = "Paperboat release that runs now."
}

output "release_revision" {
  value       = module.app.release_revision
  description = "Helm revision."
}
