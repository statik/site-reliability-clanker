terraform {
  required_version = ">= 1.10"

  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = "3.3.0"
    }
  }
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
  })]

  set_sensitive = var.admin_token == "" ? [] : [{
    name  = "adminToken"
    value = var.admin_token
  }]
}

# The route of the application on the Gateway of 40-platform. A chart, not a
# kubernetes_manifest, so that this layer needs only the helm provider.
resource "helm_release" "route" {
  name      = "paperboat-route"
  namespace = var.app_namespace
  chart     = "${path.module}/route"

  values = [yamlencode({
    name = "paperboat"
    gateway = {
      name      = var.gateway_name
      namespace = var.gateway_namespace
    }
    backend = {
      service = "paperboat-web"
      port    = 8080
    }
  })]

  depends_on = [helm_release.paperboat]
}
