output "cluster_name" {
  value       = kind_cluster.this.name
  description = "kind cluster name."
}

output "cluster_endpoint" {
  value       = kind_cluster.this.endpoint
  description = "API server URL."
}

output "cluster_ca_certificate" {
  value       = kind_cluster.this.cluster_ca_certificate
  sensitive   = true
  description = "Cluster CA certificate in PEM format."
}

output "kubeconfig_path" {
  value       = kind_cluster.this.kubeconfig_path
  description = "Administrator kubeconfig. Only the access broker reads it."
}

output "kubernetes_version" {
  value       = var.kubernetes_version
  description = "Kubernetes minor version that runs now."
}

output "node_image_version" {
  value       = local.node_image_tag
  description = "kind node image tag that runs now."
}
