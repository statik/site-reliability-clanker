variable "app" {
  type = object({
    # Name of the pack. The releases are <name>, <name>-platform and <name>-extras.
    name = string
    # Release of the application to run.
    version = string
    # Whether the pack has values files, an extras chart and release records.
    values  = bool
    extras  = bool
    records = bool
    # Helm timeout of an apply, in seconds.
    timeout_seconds = number
    # Keys of the Secret <name>-generated. Each gets a random value.
    secrets = list(string)
    # Helm value path to the value, as Helm --set reads it.
    settings = map(string)
    route = object({
      service = string
      port    = number
      # Path prefixes. Empty: every path.
      paths = list(string)
      # Request timeout. 0 is no timeout; null is the default of the gateway.
      timeout_seconds = number
    })
  })
  description = "The application of the pack. The file app.tf of the stack writes it from the manifest."

  validation {
    condition     = can(regex("^[0-9]+[.][0-9]+[.][0-9]+(-[a-z][a-z0-9]*)?$", var.app.version))
    error_message = "app.version must be a release such as 1.2.3 or 1.2.0-bad."
  }
}

variable "cloud" {
  type        = string
  description = "Target of the stack: local, aws, gcp or azure. It picks values-<cloud>.yaml."

  validation {
    condition     = contains(["local", "aws", "gcp", "azure"], var.cloud)
    error_message = "cloud must be local, aws, gcp or azure."
  }
}

variable "pack_dir" {
  type        = string
  description = "Directory of the deploy files of the pack, apps/<name> of the platform repository, as a path relative to the stack."
}

variable "image_registry" {
  type        = string
  description = "Registry host and path prefix for the images of the application."
}

variable "app_namespace" {
  type        = string
  description = "Namespace of the application."
}

variable "app_secret_name" {
  type        = string
  description = "Secret <name>-db with the credential of the database role of the application."
}

variable "shared_volume_claim" {
  type        = string
  description = "ReadWriteMany claim of the application."
}

variable "gateway_name" {
  type        = string
  description = "Name of the Gateway of 40-platform."
}

variable "gateway_namespace" {
  type        = string
  description = "Namespace of the Gateway of 40-platform."
}

variable "public_address" {
  type        = string
  description = "Host name or IP address that serves the application."
}

variable "admin_token" {
  type        = string
  sensitive   = true
  description = "Bearer token for fault injection in demo environments. Empty turns it off."
  default     = ""
}
