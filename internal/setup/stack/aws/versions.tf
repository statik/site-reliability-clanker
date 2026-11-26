terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.73.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "4.4.2"
    }
  }

  # The owner keeps the state: it holds the database password and the signing key. Set the
  # file with `tofu init -backend-config=path=<file>`.
  backend "local" {}
}
