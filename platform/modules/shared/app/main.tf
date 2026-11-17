terraform {
  required_version = ">= 1.10"

  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = "3.3.0"
    }
  }
}

locals {
  use_nodeport = var.gateway_class == "nodeport"
}

resource "helm_release" "paperboat" {
  name      = "paperboat"
  namespace = var.app_namespace
  chart     = var.chart_path

  wait          = true
  wait_for_jobs = true
  timeout       = 300

  values = [yamlencode({
    image = {
      registry = var.image_registry
      tag      = var.paperboat_version
    }
    api     = { replicas = var.api_replicas }
    worker  = { replicas = var.worker_replicas }
    web     = { replicas = var.web_replicas }
    loadgen = { enabled = var.loadgen_enabled }
    database = {
      secretName = var.app_secret_name
    }
    sharedVolume = {
      claimName = var.shared_volume_claim
    }
    service = local.use_nodeport ? { type = "NodePort", nodePort = 30080 } : { type = "ClusterIP" }
    gateway = local.use_nodeport ? {} : { className = var.gateway_class }
  })]

  set_sensitive = var.admin_token == "" ? [] : [{
    name  = "adminToken"
    value = var.admin_token
  }]
}
