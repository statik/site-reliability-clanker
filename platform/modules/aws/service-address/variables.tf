variable "cluster_name" {
  description = "EKS cluster name."
  type        = string
}

variable "cluster_endpoint" {
  description = "API server URL of the cluster."
  type        = string
}

variable "cluster_ca_data" {
  description = "Cluster CA certificate, PEM in base64."
  type        = string
}

variable "region" {
  description = "AWS region of the cluster."
  type        = string
}

variable "service" {
  description = "The Service of type LoadBalancer, as <namespace>/<name>."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]+/[a-z0-9-]+$", var.service))
    error_message = "service must be <namespace>/<name>."
  }
}
