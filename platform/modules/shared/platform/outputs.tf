output "app_namespace" {
  value       = kubernetes_namespace_v1.app.metadata[0].name
  description = "Namespace for Paperboat."
}

output "gateway_class" {
  value       = var.gateway_class
  description = "GatewayClass name, or nodeport."
}

output "public_address" {
  value       = var.public_address != "" ? var.public_address : local.gateway_address
  description = "Host name or IP address that serves the application."
}

output "app_secret_name" {
  value       = kubernetes_secret_v1.database.metadata[0].name
  description = "Secret with the key DATABASE_URL."
}

output "shared_volume_claim" {
  value       = kubernetes_persistent_volume_claim_v1.shared.metadata[0].name
  description = "ReadWriteMany claim for api and worker."
}

output "collector_namespace" {
  value       = try(kubernetes_namespace_v1.observe[0].metadata[0].name, "")
  description = "Namespace of the collector. Empty when there is no collector."
}
