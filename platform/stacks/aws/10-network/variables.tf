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

variable "network_cidr" {
  description = "Address range for the environment."
  type        = string
}

variable "zone_count" {
  description = "Number of availability zones to use."
  type        = number
}
