variable "env_name" {
  description = "Environment name, for example gcp-sandbox."
  type        = string
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

variable "app_namespace" {
  description = "Namespace for Paperboat."
  type        = string
  default     = "paperboat"
}

variable "public_port" {
  description = "Port of the public listener."
  type        = number
  default     = 80
}

variable "collector_endpoint" {
  description = "Address of the control plane for the data of the collector, for example http://host:7070/otlp. Empty: no collector."
  type        = string
  default     = ""
}

variable "collector_token" {
  description = "Token that the collector sends to the control plane."
  type        = string
  sensitive   = true
  default     = ""
}
