variable "env_name" {
  description = "Environment name. Used in role and policy names and in state object keys."
  type        = string
}

variable "broker_principal" {
  description = "ARN of the IAM principal that the broker runs as. Only it can assume the roles."
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:(iam|sts)::[0-9]{12}:.+$", var.broker_principal))
    error_message = "broker_principal must be the ARN of an IAM role or user. It cannot be empty."
  }
}

variable "applier_layers" {
  description = "Layers that get an applier role."
  type        = list(string)
}

variable "state_bucket_arn" {
  description = "ARN of the OpenTofu state bucket."
  type        = string
}

variable "evidence_bucket_arn" {
  description = "ARN of the audit evidence bucket."
  type        = string
}
