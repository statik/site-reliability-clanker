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

variable "app_name" {
  description = "Name of the application pack. The namespace, the database role, the database, the Secret <name>-db and the claim <name>-shared have this name."
  type        = string
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
