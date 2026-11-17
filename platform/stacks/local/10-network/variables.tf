variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "network_cidr" {
  type        = string
  description = "Address range for the Docker network."
}

variable "zone_count" {
  type        = number
  description = "Not used on the local target."
  default     = 1
}

variable "registry_port" {
  type        = number
  description = "Host port of the image registry."
  default     = 5001
}
