variable "env_name" {
  type        = string
  description = "Environment name, for example azure-sandbox."
}

variable "state_location" {
  type        = string
  description = "State backend as <storage account>/<container>."
}

variable "subscription_id" {
  type        = string
  description = "Azure subscription that holds the environment."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "kubernetes_version" {
  type        = string
  description = "Kubernetes minor version, for example 1.34."
}

variable "node_image_version" {
  type        = string
  description = "Node OS image version. Empty means the AKS default. AKS cannot pin an image at create time."
}

variable "node_count" {
  type        = number
  description = "Number of worker nodes."
}

variable "node_size" {
  type        = string
  description = "VM size of the worker nodes."
}
