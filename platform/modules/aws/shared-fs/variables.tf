variable "name" {
  description = "Name of the file system."
  type        = string
}

variable "vpc_id" {
  description = "VPC of the mount targets."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnets. Each one gets a mount target. One subnet per zone."
  type        = list(string)
}

variable "client_security_group_id" {
  description = "Security group that can mount the file system."
  type        = string
}
