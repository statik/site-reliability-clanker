output "cluster_name" {
  value       = module.cluster.cluster_name
  description = "AKS cluster name."
}

output "cluster_endpoint" {
  value       = module.cluster.cluster_endpoint
  description = "API server URL."
}

output "cluster_ca_certificate" {
  value       = module.cluster.cluster_ca_certificate
  description = "Cluster CA certificate, PEM."
  sensitive   = true
}

output "kubeconfig_path" {
  value       = ""
  description = "Empty. Clients authenticate with kubelogin exec."
}

output "kubernetes_version" {
  value       = module.cluster.kubernetes_version
  description = "Kubernetes version that runs now."
}

output "node_image_version" {
  value       = module.cluster.node_image_version
  description = "Node image that runs now."
}

output "cluster_id" {
  value       = module.cluster.cluster_id
  description = "AKS resource ID. Namespace-scoped role assignments start with it."
}

output "oidc_issuer_url" {
  value       = module.cluster.oidc_issuer_url
  description = "Issuer for Entra Workload ID federated credentials."
}

output "node_resource_group" {
  value       = module.cluster.node_resource_group
  description = "Resource group that AKS manages. The public address is in it."
}

output "public_address" {
  value       = module.cluster.public_address
  description = "Static public IP address for the gateway load balancer."
}
