variable "env_name" {
  description = "Environment name. The same name as in 00-bootstrap, for example prod."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,22}[a-z0-9]$", var.env_name))
    error_message = "env_name must be 3 to 24 lowercase letters, digits or hyphens."
  }
}

variable "region" {
  description = "AWS region of the environment."
  type        = string
}

variable "vpc_id" {
  description = "ID of the existing VPC. This stack changes nothing in it."
  type        = string

  validation {
    condition     = can(regex("^vpc-[0-9a-f]+$", var.vpc_id))
    error_message = "vpc_id must be the ID of a VPC."
  }
}

variable "private_subnet_ids" {
  description = "Private subnets of the VPC with a route to a NAT gateway, in two zones or more."
  type        = list(string)

  validation {
    condition     = length(var.private_subnet_ids) >= 2
    error_message = "private_subnet_ids must name two subnets or more, in two zones."
  }

  validation {
    condition = alltrue([
      for id in var.private_subnet_ids : can(regex("^subnet-[0-9a-f]+$", id))
    ])
    error_message = "Each item of private_subnet_ids must be the ID of a subnet."
  }
}

variable "router_security_group_id" {
  description = "Security group of the Tailscale subnet router. The load balancer admits it."
  type        = string

  validation {
    condition     = can(regex("^sg-[0-9a-f]+$", var.router_security_group_id))
    error_message = "router_security_group_id must be the ID of a security group."
  }
}

variable "zone_id" {
  description = "ID of the existing public Route 53 zone for the names of the core and of Keycloak."
  type        = string
}

variable "required_tags" {
  description = <<-EOT
    Tags that the organization requires on each resource, for example on the request of
    ec2:RunInstances. They go on every resource and on the instance and volume of the core.
    Use {} when the account requires none.
  EOT
  type        = map(string)
}

variable "keycloak_realm" {
  description = <<-EOT
    The realm that Keycloak imports, as JSON text: the content of
    internal/setup/stack/keycloak-realm.json, which the installer gives. Keycloak replaces
    each $${NAME} in it with a variable of the container.
  EOT
  type        = string

  validation {
    condition = (
      can(jsondecode(var.keycloak_realm).realm) &&
      can(jsondecode(var.keycloak_realm).clients[0].clientId)
    )
    error_message = "keycloak_realm must be a realm in JSON with a name and one client or more."
  }
}

variable "core_instance_type" {
  description = "Instance type of the core. It must match the architecture of the image."
  type        = string
  default     = "t4g.small"
}

variable "runner_image_digest" {
  description = <<-EOT
    Phase 2. Digest of the runner image in the repository that this stack creates, for
    example sha256:<64 hex digits>. Without it there is no task definition for the runners.
  EOT
  type        = string
  default     = null

  validation {
    condition = (
      var.runner_image_digest == null ||
      can(regex("^sha256:[0-9a-f]{64}$", var.runner_image_digest))
    )
    error_message = "runner_image_digest must be sha256: and 64 hexadecimal digits."
  }
}

variable "core_image_id" {
  description = <<-EOT
    Phase 2. ID of the Nanos image (AMI) of the core. Without it there is no instance. It
    needs runner_image_digest.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.core_image_id == null || can(regex("^ami-[0-9a-f]+$", var.core_image_id))
    error_message = "core_image_id must be the ID of an image."
  }
}

variable "application_name" {
  description = <<-EOT
    Name of the application pack. Its images are <image_registry>/<application_name>, in a
    repository of this stack. The installer gives the name of the pack of its checkout.
  EOT
  type        = string
  default     = "paperboat"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,39}$", var.application_name))
    error_message = "application_name must be lowercase letters, digits and hyphens."
  }
}

variable "model_api_key" {
  description = <<-EOT
    OpenRouter API key of the core: the parameter OPENROUTER_API_KEY. null: no parameter, and
    the core answers with recorded responses instead of a hosted model.
  EOT
  type        = string
  default     = null
  sensitive   = true
}
