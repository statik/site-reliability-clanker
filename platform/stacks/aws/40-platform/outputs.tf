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
  description = "DNS name of the load balancer that serves the application."
  value       = module.load_balancer.address

  # The first apply waits for the address. A later plan reads it, and the Service can lose it.
  precondition {
    condition = module.load_balancer.address != ""
    error_message = format(
      "The Envoy Service %s has no load balancer address yet. Read the events of the Service.",
      module.platform.envoy_service,
    )
  }
}

output "public_port" {
  description = "Port of the public listener."
  value       = module.platform.public_port
}

output "app_domain" {
  description = "Domain of the host names of the applications. Empty: this target has none yet."
  value       = module.platform.app_domain
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
