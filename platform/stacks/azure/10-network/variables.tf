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

variable "network_cidr" {
  type        = string
  description = "Address range for the environment. Use a /16 to /20 range outside 172.16.0.0/16 and 192.168.0.0/16."

  validation {
    condition     = can(cidrhost(var.network_cidr, 0)) && tonumber(split("/", var.network_cidr)[1]) <= 20
    error_message = "network_cidr must be a valid IPv4 CIDR of size /20 or larger."
  }
}

variable "zone_count" {
  type        = number
  description = "Number of availability zones to use, 1 to 3."

  validation {
    condition     = contains([1, 2, 3], var.zone_count)
    error_message = "zone_count must be 1, 2 or 3."
  }
}
