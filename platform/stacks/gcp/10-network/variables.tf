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

variable "network_cidr" {
  description = "Address range for the environment, /18 or larger."
  type        = string
}

variable "zone_count" {
  description = "Number of zones to use. 1 gives a zonal cluster in layer 20-cluster."
  type        = number
}
