output "cluster_name" {
  description = "Cluster name."
  value       = google_container_cluster.this.name
}

output "cluster_location" {
  description = "Zone of a zonal cluster or region of a regional cluster."
  value       = google_container_cluster.this.location
}

output "cluster_endpoint" {
  description = "API server URL."
  value       = "https://${google_container_cluster.this.endpoint}"
}

output "cluster_ca_certificate" {
  description = "Cluster CA certificate in PEM format."
  value       = base64decode(google_container_cluster.this.master_auth[0].cluster_ca_certificate)
  sensitive   = true
}

output "kubernetes_version" {
  description = "Control plane version that runs now."
  value       = google_container_cluster.this.master_version
}

output "node_image_version" {
  description = "GKE version of the nodes, which sets the node OS image."
  value       = google_container_node_pool.default.version
}
