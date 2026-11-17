output "app_namespace" {
  value       = module.platform.app_namespace
  description = "Namespace for Paperboat."
}

output "gateway_class" {
  value       = module.platform.gateway_class
  description = "GatewayClass name."
}

output "public_address" {
  value       = module.platform.public_address
  description = "IP address that serves the application."
}

output "app_secret_name" {
  value       = module.platform.app_secret_name
  description = "Kubernetes secret in app_namespace with the key DATABASE_URL."
}

output "shared_volume_claim" {
  value       = module.platform.shared_volume_claim
  description = "ReadWriteMany PersistentVolumeClaim in app_namespace."
}
