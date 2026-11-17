variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "kubernetes_version" {
  type        = string
  description = "Kubernetes minor version."

  validation {
    condition     = contains(keys(local.node_images), var.kubernetes_version)
    error_message = "Use a kubernetes_version that has a kind node image: 1.35, 1.36 or 1.37."
  }
}

variable "node_image_version" {
  type        = string
  description = "Full kind node image tag. Empty selects the default for kubernetes_version."
  default     = ""
}

variable "node_count" {
  type        = number
  description = "Number of worker nodes in addition to the control-plane node."
  default     = 0
}

variable "node_size" {
  type        = string
  description = "Not used on the local target."
  default     = ""
}

variable "public_host_port" {
  type        = number
  description = "Host port that maps to node port 30080."
  default     = 8088
}
