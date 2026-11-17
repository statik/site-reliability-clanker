variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "paperboat_version" {
  type        = string
  description = "Paperboat release to run."
}

variable "api_replicas" {
  type        = number
  description = "Replicas of api."
}

variable "worker_replicas" {
  type        = number
  description = "Replicas of worker."
}

variable "web_replicas" {
  type        = number
  description = "Replicas of web."
}

variable "loadgen_enabled" {
  type        = bool
  description = "Run synthetic traffic."
}

variable "workload_runtime" {
  type        = string
  description = "kubernetes or unikernel. Only kubernetes is implemented."
  default     = "kubernetes"
}

variable "admin_token" {
  type        = string
  sensitive   = true
  description = "Bearer token for fault injection. The simulator uses it in demo mode."
  default     = ""
}
