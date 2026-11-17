variable "env_name" {
  description = "Environment name, for example aws-sandbox."
  type        = string
}

variable "state_location" {
  description = "Name of the S3 bucket that holds OpenTofu state."
  type        = string
}

variable "region" {
  description = "AWS region of the environment."
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
  description = "EC2 instance type of the worker nodes."
  type        = string
}
