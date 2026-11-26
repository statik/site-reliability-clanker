# Stands for the state bucket of the real layer. The check makes the bucket in MinIO.
variable "name" {
  type = string
}

resource "terraform_data" "bucket" {
  input = var.name
}

output "name" {
  value = terraform_data.bucket.output
}
