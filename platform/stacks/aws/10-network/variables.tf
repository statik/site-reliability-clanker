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
  description = "Address range for the environment. Not used when existing_network_id is set."
  type        = string
}

variable "zone_count" {
  description = "Number of availability zones to use. Not used when existing_network_id is set."
  type        = number
}

variable "existing_network_id" {
  description = <<-EOT
    ID of a VPC that exists. When it is set, this layer makes no VPC, subnet, NAT gateway,
    internet gateway or route table, and passes this ID and existing_private_subnet_ids on
    to the layers above. Empty: the layer makes a new VPC from network_cidr.
  EOT
  type        = string
  default     = ""

  validation {
    condition = (
      var.existing_network_id == "" || can(regex("^vpc-[0-9a-f]+$", var.existing_network_id))
    )
    error_message = "existing_network_id must be empty or a VPC ID such as vpc-0abc."
  }
}

variable "existing_private_subnet_ids" {
  description = <<-EOT
    Private subnets of existing_network_id for the nodes, the data services and the internal
    load balancer. Give one subnet in each of two or more availability zones. Only with
    existing_network_id.
  EOT
  type        = list(string)
  default     = []

  validation {
    condition = (
      var.existing_network_id == ""
      ? length(var.existing_private_subnet_ids) == 0
      : length(var.existing_private_subnet_ids) >= 2
    )
    error_message = "Give two or more subnets with existing_network_id, and none without it."
  }

  validation {
    condition = alltrue([
      for id in var.existing_private_subnet_ids : can(regex("^subnet-[0-9a-f]+$", id))
    ])
    error_message = "Each of existing_private_subnet_ids must be a subnet ID such as subnet-0abc."
  }
}

variable "image_registry" {
  description = <<-EOT
    Registry host and path prefix of the Paperboat images, for example the ECR registry that
    the installer makes. A Paperboat image is <image_registry>/paperboat:<version>.
  EOT
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)*$", var.image_registry))
    error_message = "image_registry must be a host and optional path, no scheme, no trailing /."
  }
}
