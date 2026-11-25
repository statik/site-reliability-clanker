output "network_id" {
  description = "VPC ID."
  value       = local.existing ? var.existing_network_id : module.network[0].vpc_id
}

output "private_subnet_ids" {
  description = "Subnets for nodes, data services and the internal load balancer."
  value = (
    local.existing ? var.existing_private_subnet_ids : module.network[0].private_subnet_ids
  )
}

output "public_subnet_ids" {
  description = "Public subnets. Empty with existing_network_id: the load balancer is internal."
  value       = local.existing ? [] : module.network[0].public_subnet_ids
}

output "image_registry" {
  description = "Registry host and path prefix. An image is <image_registry>/paperboat:<version>."
  value       = var.image_registry
}
