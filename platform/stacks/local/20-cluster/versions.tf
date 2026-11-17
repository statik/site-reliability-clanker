terraform {
  required_version = ">= 1.10"

  required_providers {
    kind = {
      source  = "tehcyx/kind"
      version = "0.13.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "2.9.1"
    }
  }

  backend "local" {}
}
