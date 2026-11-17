variable "name" {
  description = "Name prefix for the network resources."
  type        = string
}

variable "cidr" {
  description = "VPC address range. The module cuts it into 16 equal subnets."
  type        = string

  validation {
    condition     = can(cidrsubnet(var.cidr, 4, 15))
    error_message = "cidr must be a valid IPv4 CIDR block that can be split into 16 subnets."
  }
}

variable "zone_count" {
  description = "Number of availability zones. EKS and RDS need at least 2."
  type        = number

  validation {
    condition     = contains([2, 3, 4, 5, 6, 7, 8], var.zone_count)
    error_message = "zone_count must be a whole number from 2 to 8. EKS and RDS need 2 zones."
  }
}
