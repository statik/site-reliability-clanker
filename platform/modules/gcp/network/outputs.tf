output "network_id" {
  description = "ID of the VPC network."
  value       = google_compute_network.this.id
}

output "private_subnet_id" {
  description = "ID of the subnet for nodes."
  value       = google_compute_subnetwork.private.id
}

output "pods_range_name" {
  description = "Name of the secondary range for pods."
  value       = local.pods_range_name
}

output "services_range_name" {
  description = "Name of the secondary range for Kubernetes services."
  value       = local.services_range_name
}

output "private_services_range_name" {
  description = "Name of the range that is allocated for private services access."
  value       = google_service_networking_connection.private_services.reserved_peering_ranges[0]
}

output "zones" {
  description = "Zones that the environment uses."
  value       = slice(sort(data.google_compute_zones.available.names), 0, var.zone_count)
}

output "public_address" {
  description = "Reserved regional IP address for the public load balancer."
  value       = google_compute_address.public.address
}
