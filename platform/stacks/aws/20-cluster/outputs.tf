output "cluster_name" {
  description = "EKS cluster name."
  value       = module.cluster.name
}

output "cluster_endpoint" {
  description = "API server URL."
  value       = module.cluster.endpoint
}

output "cluster_ca_certificate" {
  description = "Cluster CA certificate. PEM, not base64."
  value       = module.cluster.ca_certificate_pem
  sensitive   = true
}

output "cluster_certificate_authority_data" {
  description = <<-EOT
    Cluster CA certificate, PEM in base64, as a kubeconfig takes it. Not secret: the broker
    builds the kubeconfig of a debug session or a runbook from this output.
  EOT
  value       = module.cluster.ca_certificate_data
}

output "kubeconfig_path" {
  description = "Empty. Use exec authentication: aws eks get-token --cluster-name <cluster_name>."
  value       = ""
}

output "kubernetes_version" {
  description = "Kubernetes version that runs now."
  value       = module.cluster.kubernetes_version
}

output "node_image_version" {
  description = "EKS AMI release version that the nodes run now."
  value       = module.cluster.node_image_version
}

output "node_security_group_id" {
  description = "Security group of the nodes. 30-data lets it reach the database and file system."
  value       = module.cluster.node_security_group_id
}
