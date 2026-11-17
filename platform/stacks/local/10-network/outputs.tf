output "network_id" {
  value       = docker_network.this.name
  description = "Docker network name. The runner passes it to kind."
}

output "private_subnet_ids" {
  value       = []
  description = "The local target has no subnets."
}

output "public_subnet_ids" {
  value       = []
  description = "The local target has no subnets."
}

output "image_registry" {
  value       = "localhost:${var.registry_port}"
  description = "Registry host for Paperboat images."
}

output "registry_internal_host" {
  value       = "${docker_container.registry.name}:5000"
  description = "Registry address on the Docker network, for the cluster nodes."
}
