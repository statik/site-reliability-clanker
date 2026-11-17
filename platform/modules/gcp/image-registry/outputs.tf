output "image_registry" {
  description = "Registry host and path prefix for images."
  value = join("/", [
    "${google_artifact_registry_repository.this.location}-docker.pkg.dev",
    google_artifact_registry_repository.this.project,
    google_artifact_registry_repository.this.repository_id,
  ])
}
