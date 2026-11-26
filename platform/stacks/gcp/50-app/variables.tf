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
