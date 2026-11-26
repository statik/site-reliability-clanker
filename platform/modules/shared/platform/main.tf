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
}

locals {
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "40-platform"
  }
  application_label  = { "clanker.dev/application" = "true" }
  gateway_name       = "clanker"
  gateway_namespace  = "clanker-gateway"
  envoy_namespace    = "envoy-gateway-system"
  envoy_service_name = "clanker-envoy"
}

resource "kubernetes_namespace_v1" "app" {
  metadata {
    name   = var.app_name
    labels = merge(local.labels, local.application_label)
  }
}

resource "kubernetes_persistent_volume_claim_v1" "shared" {
  metadata {
    name      = "${var.app_name}-shared"
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

# The entry point of the target. Envoy Gateway brings the CRDs of the Gateway API and of
# Envoy Gateway. The objects of those kinds are in a chart that a second release installs:
# a kubernetes_manifest needs the CRD at plan time, and on a new cluster the CRD does not
# exist before the apply of this layer.
resource "kubernetes_namespace_v1" "gateway" {
  metadata {
    name   = local.gateway_namespace
    labels = local.labels
  }
}

resource "helm_release" "envoy_gateway" {
  name             = "envoy-gateway"
  namespace        = local.envoy_namespace
  create_namespace = true
  repository       = "oci://docker.io/envoyproxy"
  chart            = "gateway-helm"
  version          = "v1.10.0"
}

resource "helm_release" "gateway" {
  name      = local.gateway_name
  namespace = kubernetes_namespace_v1.gateway.metadata[0].name
  chart     = "${path.module}/gateway"

  # Through values, not set: a set value with a comma is a list for Helm, and the AWS
  # annotations hold commas.
  values = [yamlencode({
    gateway = {
      name                 = local.gateway_name
      className            = "envoy"
      port                 = var.public_port
      routeNamespaceLabels = local.application_label
    }
    envoyProxyName = "clanker"
    envoyService = {
      name        = local.envoy_service_name
      type        = var.node_port > 0 ? "NodePort" : "LoadBalancer"
      nodePort    = var.node_port
      annotations = var.service_annotations
    }
  })]

  depends_on = [helm_release.envoy_gateway]
}

# The Gateway admits routes from the application namespaces. The admin ClusterRole has no
# rights on routes, and the applier of 50-app has only admin in its namespace.
resource "kubernetes_cluster_role_v1" "route_admin" {
  metadata {
    name = "clanker-httproute-admin"
    labels = merge(local.labels, {
      "rbac.authorization.k8s.io/aggregate-to-admin" = "true"
    })
  }

  rule {
    api_groups = ["gateway.networking.k8s.io"]
    resources  = ["httproutes"]
    verbs      = ["create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"]
  }
}

# A cloud load balancer gets its address after Envoy Gateway makes the Service, so the
# address is read after the apply of the gateway release.
data "kubernetes_service_v1" "envoy" {
  count = var.public_address == "" ? 1 : 0

  metadata {
    name      = local.envoy_service_name
    namespace = local.envoy_namespace
  }

  depends_on = [helm_release.gateway]
}

locals {
  envoy_ingress = try(data.kubernetes_service_v1.envoy[0].status[0].load_balancer[0].ingress[0], {})
  cloud_address = try(compact([
    try(local.envoy_ingress.hostname, ""), try(local.envoy_ingress.ip, ""),
  ])[0], "")
  public_address = var.public_address != "" ? var.public_address : local.cloud_address
}

# The collector of the cluster. It reads the state of the workloads in the namespace of the
# application and sends it to the control plane. The token is not in the configuration: the
# collector reads it from its environment.
locals {
  collector_count = var.collector_endpoint != "" ? 1 : 0
  collector_name  = "clanker-collector"
  collector_selector = {
    "app.kubernetes.io/name" = local.collector_name
  }
  collector_config = yamlencode({
    receivers = {
      k8s_cluster = {
        auth_type                   = "serviceAccount"
        collection_interval         = "15s"
        namespaces                  = [kubernetes_namespace_v1.app.metadata[0].name]
        node_conditions_to_report   = []
        allocatable_types_to_report = []
      }
    }
    exporters = {
      otlp_http = {
        endpoint    = var.collector_endpoint
        encoding    = "json"
        compression = "none"
        headers = {
          Authorization = "Bearer $${env:COLLECTOR_TOKEN}"
        }
      }
    }
    service = {
      pipelines = {
        metrics = {
          receivers = ["k8s_cluster"]
          exporters = ["otlp_http"]
        }
      }
    }
  })
}

resource "kubernetes_namespace_v1" "observe" {
  count = local.collector_count

  metadata {
    name   = "clanker-observe"
    labels = local.labels
  }
}

resource "kubernetes_service_account_v1" "collector" {
  count = local.collector_count

  metadata {
    name      = local.collector_name
    namespace = kubernetes_namespace_v1.observe[0].metadata[0].name
    labels    = local.labels
  }
}

resource "kubernetes_role_binding_v1" "collector_view" {
  count = local.collector_count

  metadata {
    name      = local.collector_name
    namespace = kubernetes_namespace_v1.app.metadata[0].name
    labels    = local.labels
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = "view"
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account_v1.collector[0].metadata[0].name
    namespace = kubernetes_namespace_v1.observe[0].metadata[0].name
  }
}

resource "kubernetes_secret_v1" "collector" {
  count = local.collector_count

  metadata {
    name      = "${local.collector_name}-token"
    namespace = kubernetes_namespace_v1.observe[0].metadata[0].name
    labels    = local.labels
  }

  data = {
    COLLECTOR_TOKEN = var.collector_token
  }
}

resource "kubernetes_config_map_v1" "collector" {
  count = local.collector_count

  metadata {
    name      = "${local.collector_name}-config"
    namespace = kubernetes_namespace_v1.observe[0].metadata[0].name
    labels    = local.labels
  }

  data = {
    "config.yaml" = local.collector_config
  }
}

resource "kubernetes_deployment_v1" "collector" {
  count = local.collector_count

  metadata {
    name      = local.collector_name
    namespace = kubernetes_namespace_v1.observe[0].metadata[0].name
    labels    = local.labels
  }

  spec {
    replicas = 1

    selector {
      match_labels = local.collector_selector
    }

    template {
      metadata {
        labels = merge(local.labels, local.collector_selector)
        annotations = {
          "clanker.dev/config-sha256" = sha256(local.collector_config)
        }
      }

      spec {
        service_account_name = kubernetes_service_account_v1.collector[0].metadata[0].name

        container {
          name = "collector"
          # 0.165.0
          image = "otel/opentelemetry-collector-k8s@sha256:ce55d0a38923c83120e4ba613fbdf446786652d2ce9f29d1cd7879e5d869829f"
          args  = ["--config=/etc/collector/config.yaml"]

          env {
            name = "COLLECTOR_TOKEN"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.collector[0].metadata[0].name
                key  = "COLLECTOR_TOKEN"
              }
            }
          }

          resources {
            requests = { cpu = "50m", memory = "64Mi" }
            limits   = { memory = "256Mi" }
          }

          security_context {
            run_as_non_root            = true
            allow_privilege_escalation = false
            read_only_root_filesystem  = true
            capabilities {
              drop = ["ALL"]
            }
          }

          volume_mount {
            name       = "config"
            mount_path = "/etc/collector"
            read_only  = true
          }
        }

        volume {
          name = "config"
          config_map {
            name = kubernetes_config_map_v1.collector[0].metadata[0].name
          }
        }
      }
    }
  }

  depends_on = [kubernetes_role_binding_v1.collector_view]
}
