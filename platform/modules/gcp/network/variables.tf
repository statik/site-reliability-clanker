variable "name" {
  description = "Name prefix for the network resources."
  type        = string
}

variable "region" {
  description = "Region of the subnet, the NAT gateway and the public address."
  type        = string
}

variable "network_cidr" {
  description = "Address range for the environment. The module divides it into fixed parts."
  type        = string

  validation {
    condition     = can(cidrhost(var.network_cidr, 0)) && try(tonumber(split("/", var.network_cidr)[1]) <= 18, false)
    error_message = "network_cidr must be an IPv4 CIDR block with a prefix of /18 or larger, for example 10.20.0.0/16."
  }
}

variable "zone_count" {
  description = "Number of zones to use."
  type        = number

  validation {
    condition     = contains([1, 2, 3], var.zone_count)
    error_message = "zone_count must be 1, 2 or 3."
  }
}
