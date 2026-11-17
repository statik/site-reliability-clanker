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

variable "paperboat_version" {
  type        = string
  description = "Release to run, for example 1.1.0."
}

variable "api_replicas" {
  type        = number
  description = "Replicas of api."
}

variable "worker_replicas" {
  type        = number
  description = "Replicas of worker."
}

variable "web_replicas" {
  type        = number
  description = "Replicas of web."
}

variable "loadgen_enabled" {
  type        = bool
  description = "Run synthetic traffic."
}

variable "workload_runtime" {
  type        = string
  description = "kubernetes or unikernel. Only kubernetes is implemented."

  validation {
    condition     = var.workload_runtime == "kubernetes"
    error_message = "Only workload_runtime = \"kubernetes\" is implemented."
  }
}
