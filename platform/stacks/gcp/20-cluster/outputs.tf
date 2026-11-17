output "cluster_name" {
  description = "Cluster name."
  value       = module.kubernetes.cluster_name
}

output "cluster_endpoint" {
  description = "API server URL."
  value       = module.kubernetes.cluster_endpoint
}

output "cluster_ca_certificate" {
  description = "Cluster CA certificate in PEM format."
  value       = module.kubernetes.cluster_ca_certificate
  sensitive   = true
}

output "kubeconfig_path" {
  description = "Empty. Clients get a token from Google Cloud credentials."
  value       = ""
}

output "kubernetes_version" {
  description = "Control plane version that runs now."
  value       = module.kubernetes.kubernetes_version
}

output "node_image_version" {
  description = "GKE version of the nodes, which sets the node OS image."
  value       = module.kubernetes.node_image_version
}

output "cluster_location" {
  description = "Zone of a zonal cluster or region of a regional cluster."
  value       = module.kubernetes.cluster_location
}
