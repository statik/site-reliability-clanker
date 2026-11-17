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
  }
}

locals {
  use_envoy = var.gateway_class == "envoy"
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "40-platform"
  }
  database_url = format(
    "postgres://postgres:%s@%s:%d/%s?sslmode=prefer",
    var.postgres_admin_password, var.postgres_host, var.postgres_port, var.postgres_database,
  )
}

resource "kubernetes_namespace_v1" "app" {
  metadata {
    name   = var.app_namespace
    labels = local.labels
  }
}

resource "kubernetes_secret_v1" "database" {
  metadata {
    name      = "paperboat-db"
    namespace = kubernetes_namespace_v1.app.metadata[0].name
    labels    = local.labels
  }

  data = {
    DATABASE_URL = local.database_url
  }
}

resource "kubernetes_persistent_volume_claim_v1" "shared" {
  metadata {
    name      = "paperboat-shared"
    namespace = kubernetes_namespace_v1.app.metadata[0].name
    labels    = local.labels
  }

  spec {
    access_modes       = ["ReadWriteMany"]
    storage_class_name = var.rwx_storage_class

    resources {
      requests = { storage = "${var.shared_fs_capacity_gb}Gi" }
    }
  }
}

resource "helm_release" "envoy_gateway" {
  count = local.use_envoy ? 1 : 0

  name             = "envoy-gateway"
  namespace        = "envoy-gateway-system"
  create_namespace = true
  repository       = "oci://docker.io/envoyproxy"
  chart            = "gateway-helm"
  version          = "v1.10.0"
}

resource "kubernetes_manifest" "envoy_proxy" {
  count = local.use_envoy ? 1 : 0

  manifest = {
    apiVersion = "gateway.envoyproxy.io/v1alpha1"
    kind       = "EnvoyProxy"
    metadata = {
      name      = "paperboat"
      namespace = "envoy-gateway-system"
    }
    spec = {
      provider = {
        type = "Kubernetes"
        kubernetes = {
          envoyService = {
            annotations = var.service_annotations
          }
        }
      }
    }
  }

  depends_on = [helm_release.envoy_gateway]
}

resource "kubernetes_manifest" "gateway_class" {
  count = local.use_envoy ? 1 : 0

  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "GatewayClass"
    metadata   = { name = "envoy" }
    spec = {
      controllerName = "gateway.envoyproxy.io/gatewayclass-controller"
      parametersRef = {
        group     = "gateway.envoyproxy.io"
        kind      = "EnvoyProxy"
        name      = "paperboat"
        namespace = "envoy-gateway-system"
      }
    }
  }

  depends_on = [kubernetes_manifest.envoy_proxy]
}

resource "kubernetes_manifest" "gateway" {
  count = local.use_envoy ? 1 : 0

  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "Gateway"
    metadata = {
      name      = "paperboat"
      namespace = kubernetes_namespace_v1.app.metadata[0].name
    }
    spec = {
      gatewayClassName = "envoy"
      listeners = [{
        name     = "http"
        protocol = "HTTP"
        port     = var.public_port
      }]
    }
  }

  wait {
    fields = {
      "status.addresses[0].value" = "*"
    }
  }

  depends_on = [kubernetes_manifest.gateway_class]
}

locals {
  gateway_address = try(kubernetes_manifest.gateway[0].object.status.addresses[0].value, "")
}
