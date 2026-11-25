output "app_namespace" {
  value       = kubernetes_namespace_v1.app.metadata[0].name
  description = "Namespace for Paperboat."
}

output "gateway_name" {
  value       = local.gateway_name
  description = "Name of the Gateway that the routes of the application attach to."
}

output "gateway_namespace" {
  value       = kubernetes_namespace_v1.gateway.metadata[0].name
  description = "Namespace of the Gateway."
}

output "public_address" {
  value       = local.public_address
  description = "Address that serves the application. Empty while the load balancer has none."
}

output "envoy_service" {
  value       = "${local.envoy_namespace}/${local.envoy_service_name}"
  description = "Namespace and name of the Service of Envoy."
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
