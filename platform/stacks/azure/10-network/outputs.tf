output "network_id" {
  value       = module.network.network_id
  description = "Virtual network ID."
}

output "private_subnet_ids" {
  value = [
    module.network.subnet_ids["nodes"],
    module.network.subnet_ids["postgres"],
    module.network.subnet_ids["private-endpoints"],
  ]
  description = "Subnets for nodes and data services."
}

output "public_subnet_ids" {
  value       = tolist([])
  description = "Empty. An Azure public load balancer is not in a subnet."
}

output "image_registry" {
  value       = "${module.registry.login_server}/paperboat"
  description = "Registry host and path prefix for Paperboat images."
}

output "subnet_ids" {
  value       = module.network.subnet_ids
  description = "Keys nodes, postgres and private-endpoints."
}

output "private_dns_zone_ids" {
  value       = module.network.private_dns_zone_ids
  description = "Keys postgres and file."
}

output "zones" {
  value       = module.network.zones
  description = "Availability zones that the environment uses."
}

output "registry_id" {
  value       = module.registry.registry_id
  description = "Azure Container Registry ID."
}
