variable "env_name" {
  description = "Environment name, for example aws-sandbox."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,22}[a-z0-9]$", var.env_name))
    error_message = "env_name must be 3 to 24 lowercase letters, digits or hyphens."
  }
}

variable "state_location" {
  description = "Name of the S3 bucket for OpenTofu state. This layer creates the bucket."
  type        = string
}

variable "region" {
  description = "AWS region of the environment."
  type        = string
}

variable "broker_principal" {
  description = "ARN of the IAM principal that the broker runs as. Only it can assume the roles."
  type        = string
}

variable "core_parameter_path" {
  description = "Parameter Store path of the control plane settings. Agent roles cannot read it."
  type        = string
  default     = "/src"
}

variable "ttl_hours" {
  description = "Lifetime of the environment in hours. 0 means no TTL."
  type        = number

  validation {
    condition     = var.ttl_hours >= 0
    error_message = "ttl_hours must be 0 or more."
  }
}
