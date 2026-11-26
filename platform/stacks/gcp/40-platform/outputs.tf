output "app_namespace" {
  description = "Namespace of the application. It has the name of the pack."
  value       = module.platform.app_namespace
}

output "gateway_name" {
  description = "Name of the Gateway that the routes of the application attach to."
  value       = module.platform.gateway_name
}

output "gateway_namespace" {
  description = "Namespace of the Gateway."
  value       = module.platform.gateway_namespace
}

output "public_address" {
  description = "IP address that serves the application."
  value       = module.platform.public_address
}

output "public_port" {
  value       = module.platform.public_port
  description = "Port of the public listener."
}

output "app_domain" {
  value       = module.platform.app_domain
  description = "Domain of the host names of the applications. Empty: this target has none yet."
}

output "app_secret_name" {
  description = "Kubernetes secret in app_namespace with the credential of the database role of the application."
  value       = module.platform.app_secret_name
}

output "shared_volume_claim" {
  description = "ReadWriteMany PersistentVolumeClaim in app_namespace."
  value       = module.platform.shared_volume_claim
}

output "targets" {
  value = {
    application = module.platform.app_namespace
    data        = ""
  }
  description = "Where the application runs: the namespace of its components. The data services are not in the cluster."
}

output "collector_namespace" {
  description = "Namespace of the collector. Empty when there is no collector."
  value       = module.platform.collector_namespace
}
