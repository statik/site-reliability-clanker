variable "env_name" {
  description = "Environment name, for example gcp-sandbox."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,19}$", var.env_name))
    error_message = "env_name must be 2 to 20 characters: lowercase letters, digits and hyphens, with a letter first."
  }
}

variable "state_location" {
  description = "Name of the Cloud Storage bucket for OpenTofu state."
  type        = string
}

variable "project_id" {
  description = "Google Cloud project that holds the environment."
  type        = string
}

variable "region" {
  description = "Google Cloud region, for example us-central1."
  type        = string
}

variable "kubernetes_version" {
  description = "Kubernetes minor version, for example 1.36. The minimum is 1.36."
  type        = string
}

variable "node_image_version" {
  description = <<-EOT
    Full GKE version for the nodes, for example 1.36.4-gke.1469000. On GKE this version
    sets the node OS image. Empty means the control plane version.
  EOT
  type        = string
}

variable "node_count" {
  description = "Number of worker nodes. A regional cluster rounds up to the same number in each zone."
  type        = number
}

variable "node_size" {
  description = "Compute Engine machine type of the nodes."
  type        = string
}

variable "deletion_protection" {
  description = "Refuse to destroy the cluster. Set false for an environment that has a TTL."
  type        = bool
  default     = true
}
