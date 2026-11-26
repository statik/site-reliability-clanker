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

variable "app_name" {
  type        = string
  description = "Name of the application pack. The namespace, the database role, the database, the Secret <name>-db and the claim <name>-shared have this name."
}

variable "public_port" {
  type        = number
  description = "Port of the public listener."
  default     = 80
}

variable "collector_endpoint" {
  type        = string
  description = "Address of the control plane for the data of the collector, for example http://host:7070/otlp. Empty: no collector."
  default     = ""
}

variable "collector_token" {
  type        = string
  sensitive   = true
  description = "Token that the collector sends to the control plane."
  default     = ""
}
