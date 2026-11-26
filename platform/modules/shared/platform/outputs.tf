output "app_namespace" {
  value       = kubernetes_namespace_v1.app.metadata[0].name
  description = "Namespace of the application. It has the name of the pack."
}

output "workloads_namespace" {
  value       = try(kubernetes_namespace_v1.workloads[0].metadata[0].name, "")
  description = "Namespace <name>-workloads for the pods that the application starts. Empty when workloads_namespace is false."
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

output "public_port" {
  value       = var.public_port
  description = "Port of the public listener."
}

output "app_domain" {
  value       = var.app_domain
  description = "Domain of the host names of the applications. Empty: no application has a host name."
}

output "envoy_service" {
  value       = "${local.envoy_namespace}/${local.envoy_service_name}"
  description = "Namespace and name of the Service of Envoy."
}

output "app_secret_name" {
  value       = kubernetes_secret_v1.database.metadata[0].name
  description = "Secret with the credential of the role of the application: PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD, PGSSLMODE and DATABASE_URL."
}

output "shared_volume_claim" {
  value       = kubernetes_persistent_volume_claim_v1.shared.metadata[0].name
  description = "ReadWriteMany claim of the application."
}

output "collector_namespace" {
  value       = try(kubernetes_namespace_v1.observe[0].metadata[0].name, "")
  description = "Namespace of the collector. Empty when there is no collector."
}
