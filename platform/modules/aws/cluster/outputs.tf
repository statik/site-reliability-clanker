output "name" {
  description = "Cluster name."
  value       = aws_eks_cluster.this.name
}

output "endpoint" {
  description = "API server URL."
  value       = aws_eks_cluster.this.endpoint
}

output "ca_certificate_pem" {
  description = "Cluster CA certificate, PEM."
  value       = base64decode(aws_eks_cluster.this.certificate_authority[0].data)
  sensitive   = true
}

output "ca_certificate_data" {
  description = <<-EOT
    Cluster CA certificate, PEM in base64, as a kubeconfig takes it. It is not secret, so the
    control plane can read it from the outputs of the layer.
  EOT
  value       = aws_eks_cluster.this.certificate_authority[0].data
}

output "kubernetes_version" {
  description = "Kubernetes version that the control plane runs."
  value       = aws_eks_cluster.this.version
}

output "node_image_version" {
  description = "EKS AMI release version that the node group runs."
  value       = aws_eks_node_group.default.release_version
}

output "node_security_group_id" {
  description = "Security group of the nodes. EKS creates it and attaches it to each node."
  value       = aws_eks_cluster.this.vpc_config[0].cluster_security_group_id
}
