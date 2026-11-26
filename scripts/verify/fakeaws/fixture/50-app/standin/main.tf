# The stand-in for modules/shared/app: one resource and no application.
variable "app" {
  type = any
}

variable "env_name" {
  type = string
}

resource "terraform_data" "layer" {
  input = "${var.env_name}-${var.app.name}"
}

output "url" {
  value = null
}

output "version" {
  value = null
}
