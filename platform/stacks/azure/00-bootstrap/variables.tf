variable "env_name" {
  type        = string
  description = "Environment name, for example azure-sandbox."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,14}[a-z0-9]$", var.env_name))
    error_message = "env_name must be 3 to 16 lowercase letters, digits or hyphens. Azure resource names are derived from it."
  }
}

variable "state_location" {
  type        = string
  description = "State backend as <storage account>/<container>. This layer creates both."

  validation {
    condition     = can(regex("^[a-z0-9]{3,24}/[a-z0-9][a-z0-9-]{2,62}$", var.state_location))
    error_message = "state_location must be <storage account>/<container>, for example clankerazuresandbox/tfstate."
  }
}

variable "broker_principal" {
  type        = string
  description = "Subject claim of the tokens that the broker presents. The federated credentials trust this subject."

  validation {
    condition     = length(var.broker_principal) > 0
    error_message = "broker_principal must not be empty on Azure."
  }
}

variable "ttl_hours" {
  type        = number
  description = "Lifetime of the environment in hours. 0 means no TTL."
}

variable "subscription_id" {
  type        = string
  description = "Azure subscription that holds the environment."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "broker_issuer" {
  type        = string
  description = "OIDC issuer URL of the broker. Azure federated credentials need an issuer and a subject."

  validation {
    condition     = startswith(var.broker_issuer, "https://")
    error_message = "broker_issuer must be an https:// URL."
  }
}
