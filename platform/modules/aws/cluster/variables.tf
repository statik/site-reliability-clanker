variable "name" {
  description = "Cluster name. Also the prefix of the IAM role names."
  type        = string
}

variable "kubernetes_version" {
  description = "Kubernetes minor version, for example 1.36."
  type        = string
}

variable "node_image_version" {
  description = "EKS AMI release version for the nodes. Empty means the latest for the version."
  type        = string
}

variable "node_count" {
  description = "Number of worker nodes."
  type        = number
}

variable "node_size" {
  description = "EC2 instance type of the worker nodes. Must be an x86_64 type."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnets for the control plane network interfaces and the nodes."
  type        = list(string)
}

variable "role_path" {
  description = "IAM path for the cluster, node and pod roles."
  type        = string
}

variable "permissions_boundary_arn" {
  description = "Permission boundary to put on each IAM role that this module creates."
  type        = string
}

variable "access_entries" {
  description = <<-EOT
    Kubernetes access for IAM principals. The key is a static label. `policy` is the name of an
    EKS access policy, for example AmazonEKSViewPolicy. An empty `namespaces` list gives the
    policy cluster scope.
  EOT
  type = map(object({
    principal_arn = string
    policy        = string
    namespaces    = list(string)
  }))
}
