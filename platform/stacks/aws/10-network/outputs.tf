output "network_id" {
  description = "VPC ID."
  value       = module.network.vpc_id
}

output "private_subnet_ids" {
  description = "Subnets for nodes and data services."
  value       = module.network.private_subnet_ids
}

output "public_subnet_ids" {
  description = "Subnets for load balancers."
  value       = module.network.public_subnet_ids
}

output "image_registry" {
  description = "ECR registry host. A Paperboat image is <image_registry>/paperboat:<version>."
  value       = module.registry.registry_host
}
