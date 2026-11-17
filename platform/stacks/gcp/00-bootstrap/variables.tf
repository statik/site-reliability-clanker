variable "env_name" {
  description = "Environment name, for example gcp-sandbox."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,19}$", var.env_name))
    error_message = "env_name must be 2 to 20 characters: lowercase letters, digits and hyphens, with a letter first."
  }
}

variable "state_location" {
  description = "Name of the Cloud Storage bucket for OpenTofu state. This layer creates it."
  type        = string
}

variable "broker_principal" {
  description = <<-EOT
    IAM member that can mint tokens for the agent identities, for example
    serviceAccount:broker@my-project.iam.gserviceaccount.com. Empty means none.
  EOT
  type        = string

  validation {
    condition = var.broker_principal == "" || can(regex(
      "^(serviceAccount|user|group|principal|principalSet):", var.broker_principal
    ))
    error_message = "broker_principal must be empty or an IAM member such as serviceAccount:<email>."
  }
}

variable "ttl_hours" {
  description = "Lifetime of the environment in hours. 0 means no TTL."
  type        = number

  validation {
    condition     = var.ttl_hours >= 0 && floor(var.ttl_hours) == var.ttl_hours
    error_message = "ttl_hours must be a whole number that is 0 or more."
  }
}

variable "project_id" {
  description = "Google Cloud project that holds the environment."
  type        = string
}

variable "region" {
  description = "Google Cloud region, for example us-central1."
  type        = string
}

variable "grant_windows" {
  description = <<-EOT
    Open grant windows. The key is an elevated identity (`debugger` or `applier-<layer>`).
    The values are RFC 3339 UTC timestamps. IAM refuses each request that an elevated
    identity makes outside its window. An identity with no entry has no usable role.
  EOT
  type = map(object({
    not_before = string
    expires    = string
  }))
  default = {}
}
