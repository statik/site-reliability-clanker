variable "name" {
  description = "Globally unique bucket name."
  type        = string
}

variable "force_destroy" {
  description = <<-EOT
    Delete every object and every version with the bucket when it is destroyed. Only for an
    environment with a TTL: without it a bucket with objects cannot be destroyed.
  EOT
  type        = bool
  default     = false
}
