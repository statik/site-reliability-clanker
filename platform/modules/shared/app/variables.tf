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

  validation {
    condition     = var.workload_runtime == "kubernetes"
    error_message = "Only workload_runtime = kubernetes is implemented."
  }
}

variable "image_registry" {
  type        = string
  description = "Registry host and path prefix for Paperboat images."
}

variable "app_namespace" {
  type        = string
  description = "Namespace for Paperboat."
}

variable "app_secret_name" {
  type        = string
  description = "Secret with the key DATABASE_URL."
}

variable "shared_volume_claim" {
  type        = string
  description = "ReadWriteMany claim for api and worker."
}

variable "gateway_class" {
  type        = string
  description = "GatewayClass name, or nodeport."
}

variable "public_address" {
  type        = string
  description = "Host name or IP address that serves the application."
}

variable "chart_path" {
  type        = string
  description = "Path of the Paperboat Helm chart."
}

variable "admin_token" {
  type        = string
  sensitive   = true
  description = "Bearer token for fault injection in demo environments. Empty turns it off."
  default     = ""
}
