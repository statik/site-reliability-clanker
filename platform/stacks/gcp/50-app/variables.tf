variable "env_name" {
  description = "Environment name, for example gcp-sandbox."
  type        = string
}

variable "state_location" {
  description = "Name of the Cloud Storage bucket for OpenTofu state."
  type        = string
}

variable "project_id" {
  description = "Google Cloud project that holds the environment."
  type        = string
}

variable "region" {
  description = "Google Cloud region, for example us-central1."
  type        = string
}

variable "paperboat_version" {
  description = "Release to run, for example 1.1.0."
  type        = string
}

variable "api_replicas" {
  description = "Replicas of api."
  type        = number
}

variable "worker_replicas" {
  description = "Replicas of worker."
  type        = number
}

variable "web_replicas" {
  description = "Replicas of web."
  type        = number
}

variable "loadgen_enabled" {
  description = "Run synthetic traffic."
  type        = bool
}

variable "workload_runtime" {
  description = "kubernetes or unikernel. Only kubernetes is implemented."
  type        = string

  validation {
    condition     = var.workload_runtime == "kubernetes"
    error_message = "workload_runtime must be kubernetes. The unikernel runtime is not implemented."
  }
}
