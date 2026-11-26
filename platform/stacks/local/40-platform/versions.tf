terraform {
  required_version = ">= 1.10"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "3.5.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "3.3.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
  }

  backend "local" {}
}
