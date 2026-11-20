output "app_namespace" {
  description = "Namespace for Paperboat."
  value       = module.platform.app_namespace
}

output "gateway_class" {
  description = "GatewayClass name."
  value       = module.platform.gateway_class
}

output "public_address" {
  description = "DNS name of the load balancer that serves the application."
  value       = module.platform.public_address
}

output "app_secret_name" {
  description = "Kubernetes secret in app_namespace with the key DATABASE_URL."
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
