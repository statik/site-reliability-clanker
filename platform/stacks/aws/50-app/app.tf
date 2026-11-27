# The control plane writes this file from the manifest of the application pack paperboat.
# Do not edit it: the seed of a new control plane writes it again.

variable "paperboat_version" {
  type        = string
  description = "Release of Paperboat to run."

  validation {
    condition     = can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+(-[a-z][a-z0-9]*)?$", var.paperboat_version))
    error_message = "paperboat_version must be a release such as 1.2.3 or 1.2.0-bad."
  }
}

variable "api_replicas" {
  type        = number
  description = "Sets the Helm value api.replicas: a whole number from 1 to 5."

  validation {
    condition     = var.api_replicas >= 1 && var.api_replicas <= 5 && floor(var.api_replicas) == var.api_replicas
    error_message = "api_replicas must be a whole number from 1 to 5."
  }
}

variable "worker_replicas" {
  type        = number
  description = "Sets the Helm value worker.replicas: a whole number from 1 to 5."

  validation {
    condition     = var.worker_replicas >= 1 && var.worker_replicas <= 5 && floor(var.worker_replicas) == var.worker_replicas
    error_message = "worker_replicas must be a whole number from 1 to 5."
  }
}

variable "web_replicas" {
  type        = number
  description = "Sets the Helm value web.replicas: a whole number from 1 to 5."

  validation {
    condition     = var.web_replicas >= 1 && var.web_replicas <= 5 && floor(var.web_replicas) == var.web_replicas
    error_message = "web_replicas must be a whole number from 1 to 5."
  }
}

variable "loadgen_enabled" {
  type        = bool
  description = "Sets the Helm value loadgen.enabled."
}

locals {
  app = {
    name            = "paperboat"
    version         = var.paperboat_version
    values          = false
    extras          = false
    records         = false
    timeout_seconds = 300
    secrets         = []
    settings = {
      "api.replicas"    = tostring(var.api_replicas)
      "worker.replicas" = tostring(var.worker_replicas)
      "web.replicas"    = tostring(var.web_replicas)
      "loadgen.enabled" = tostring(var.loadgen_enabled)
    }
    route = {
      service         = "paperboat-web"
      port            = 8080
      paths           = []
      timeout_seconds = null
      hostname        = false
      wildcard        = false
    }
  }
}

output "app_url" {
  value       = module.app.url
  description = "URL that serves Paperboat."
}

output "paperboat_version" {
  value       = module.app.version
  description = "Release of Paperboat that runs now."
}
