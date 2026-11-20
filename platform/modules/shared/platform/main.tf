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
