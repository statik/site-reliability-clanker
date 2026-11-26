output "app_namespace" {
  value       = module.platform.app_namespace
  description = "Namespace of the application. It has the name of the pack."
}

output "gateway_name" {
  value       = module.platform.gateway_name
  description = "Name of the Gateway that the routes of the application attach to."
}

output "gateway_namespace" {
  value       = module.platform.gateway_namespace
  description = "Namespace of the Gateway."
}

output "public_address" {
  value       = module.platform.public_address
  description = "Address that serves the application."
}

output "app_secret_name" {
  value       = module.platform.app_secret_name
  description = "Kubernetes secret in app_namespace with the credential of the database role of the application."
}

output "shared_volume_claim" {
  value       = module.platform.shared_volume_claim
  description = "ReadWriteMany PersistentVolumeClaim in app_namespace."
}

output "targets" {
  value = {
    application = module.platform.app_namespace
    data        = local.data_outputs.data_namespace
  }
  description = "Where the application runs: the namespace of its components and the namespace of its data services."
}

output "collector_namespace" {
  value       = module.platform.collector_namespace
  description = "Namespace of the collector. Empty when there is no collector."
}
