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

variable "kubelogin_login" {
  type        = string
  description = "kubelogin login mode for the Kubernetes and Helm providers, for example azurecli or workloadidentity."
  default     = "azurecli"
}
