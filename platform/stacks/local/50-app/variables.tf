variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "admin_token" {
  type        = string
  sensitive   = true
  description = "Bearer token for fault injection. The simulator uses it in demo mode."
  default     = ""
}
