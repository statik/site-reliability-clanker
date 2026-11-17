variable "name" {
  description = "Globally unique bucket name."
  type        = string
}

variable "location" {
  description = "Bucket location, for example a region."
  type        = string
}

variable "force_destroy" {
  description = "Delete the objects when the bucket is destroyed."
  type        = bool
  default     = false
}
