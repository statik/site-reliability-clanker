output "app_namespace" {
  description = "Namespace for Paperboat."
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
  description = "DNS name of the load balancer that serves the application."
  value       = module.platform.public_address

  # Nothing waits for the load balancer: the module reads the Service once, after the
  # release of the Gateway.
  precondition {
    condition = module.platform.public_address != ""
    error_message = format(
      "The Envoy Service %s has no load balancer address yet. Read the events of the Service.",
      module.platform.envoy_service,
    )
  }
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

output "collector_namespace" {
  description = "Namespace of the collector. Empty when there is no collector."
  value       = module.platform.collector_namespace
}
