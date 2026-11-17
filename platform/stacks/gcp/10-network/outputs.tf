output "network_id" {
  description = "ID of the VPC network."
  value       = module.network.network_id
}

output "private_subnet_ids" {
  description = "Subnets for nodes and data services."
  value       = [module.network.private_subnet_id]
}

output "public_subnet_ids" {
  description = "Subnets for load balancers. Empty, because a passthrough load balancer has no subnet."
  value       = []
}

output "image_registry" {
  description = "Registry host and path prefix for Paperboat images."
  value       = module.image_registry.image_registry
}

output "zones" {
  description = "Zones that the environment uses."
  value       = module.network.zones
}

output "pods_range_name" {
  description = "Name of the secondary subnet range for pods."
  value       = module.network.pods_range_name
}

output "services_range_name" {
  description = "Name of the secondary subnet range for Kubernetes services."
  value       = module.network.services_range_name
}

output "private_services_range_name" {
  description = "Name of the range that is allocated for private services access."
  value       = module.network.private_services_range_name
}

output "public_address" {
  description = "Reserved regional IP address for the public load balancer."
  value       = module.network.public_address
}
