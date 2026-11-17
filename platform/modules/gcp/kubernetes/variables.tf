variable "name" {
  description = "Cluster name."
  type        = string
}

variable "project_id" {
  description = "Project that holds the cluster. It names the workload identity pool."
  type        = string
}

variable "zones" {
  description = "Zones for the nodes. One zone gives a zonal cluster. More give a regional cluster."
  type        = list(string)

  validation {
    condition     = length(var.zones) >= 1
    error_message = "zones must contain at least one zone."
  }
}

variable "region" {
  description = "Region of the zones."
  type        = string
}

variable "network_id" {
  description = "ID of the VPC network."
  type        = string
}

variable "subnet_id" {
  description = "ID of the subnet for nodes."
  type        = string
}

variable "pods_range_name" {
  description = "Name of the secondary subnet range for pods."
  type        = string
}

variable "services_range_name" {
  description = "Name of the secondary subnet range for Kubernetes services."
  type        = string
}

variable "kubernetes_version" {
  description = "Kubernetes minor version, 1.36 or later."
  type        = string

  validation {
    condition = (
      can(regex("^1\\.[0-9]+$", var.kubernetes_version)) &&
      try(tonumber(split(".", var.kubernetes_version)[1]) >= 36, false)
    )
    error_message = "kubernetes_version must be a minor version, 1.36 or later. The cluster is made with no default node pool, which earlier GKE versions refuse."
  }
}

variable "node_image_version" {
  description = "Full GKE version for the nodes, for example 1.36.4-gke.1469000. Empty means the control plane version."
  type        = string
}

variable "node_count" {
  description = "Total number of worker nodes."
  type        = number

  validation {
    condition     = var.node_count >= 1
    error_message = "node_count must be 1 or more."
  }
}

variable "node_size" {
  description = "Compute Engine machine type of the nodes."
  type        = string
}

variable "node_service_account" {
  description = "Email of the service account that the nodes run as."
  type        = string
}

variable "labels" {
  description = "Labels for the node VMs. The provider default labels do not reach them."
  type        = map(string)
}

variable "deletion_protection" {
  description = "Refuse to destroy the cluster."
  type        = bool
}
