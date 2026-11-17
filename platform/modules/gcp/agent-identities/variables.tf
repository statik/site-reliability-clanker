variable "project_id" {
  description = "Project that holds the environment."
  type        = string
}

variable "state_bucket" {
  description = "Name of the OpenTofu state bucket."
  type        = string
}

variable "broker_principal" {
  description = "IAM member that can mint tokens for the agent identities. Empty means none."
  type        = string
}

variable "grant_windows" {
  description = <<-EOT
    Open grant windows. The key is an elevated identity (`debugger` or `applier-<layer>`).
    The roles of an identity that has no entry can never be used.
  EOT
  type = map(object({
    not_before = string
    expires    = string
  }))

  validation {
    condition = alltrue([
      for identity in keys(var.grant_windows) :
      contains(concat(["debugger"], [for layer in local.layers : "applier-${layer}"]), identity)
    ])
    error_message = "Each grant_windows key must be `debugger` or `applier-<layer>` for a layer from 10-network to 50-app."
  }

  validation {
    condition = alltrue([
      for window in values(var.grant_windows) :
      can(timecmp(window.not_before, window.expires)) && can(regex("Z$", window.not_before)) && can(regex("Z$", window.expires))
    ])
    error_message = "Each grant window needs RFC 3339 UTC timestamps that end in Z, for example 2026-11-21T02:00:00Z."
  }
}
