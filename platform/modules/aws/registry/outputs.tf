output "registry_host" {
  description = "Registry host without the repository name."
  value       = split("/", aws_ecr_repository.this.repository_url)[0]
}

output "repository_url" {
  description = "Registry host and repository name."
  value       = aws_ecr_repository.this.repository_url
}
