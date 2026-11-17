variable "env_name" {
  description = "Environment name, for example aws-sandbox."
  type        = string
}

variable "state_location" {
  description = "Name of the S3 bucket that holds OpenTofu state."
  type        = string
}

variable "region" {
  description = "AWS region of the environment."
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
