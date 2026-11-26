# The rights of the runbook and debugger identities in Kubernetes: the rights that the local
# target gives them in the application namespace (internal/localtarget/identities-app.yaml).
# 20-cluster maps each role to its group with no access policy, so a group has only these
# rights. The data services are not in the cluster, so no data namespace gets a role.
locals {
  runbook_group  = "clanker-runbook"
  debugger_group = "clanker-debugger"
}

resource "kubernetes_role_v1" "runbook" {
  metadata {
    name      = "clanker-runbook"
    namespace = module.platform.app_namespace

    labels = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
    }
  }

  rule {
    api_groups = ["apps"]
    resources  = ["deployments"]
    verbs      = ["get", "list", "patch"]
  }

  rule {
    api_groups = [""]
    resources  = ["pods", "pods/log"]
    verbs      = ["get", "list"]
  }
}

resource "kubernetes_role_binding_v1" "runbook" {
  metadata {
    name      = "clanker-runbook"
    namespace = module.platform.app_namespace

    labels = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
    }
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Role"
    name      = kubernetes_role_v1.runbook.metadata[0].name
  }

  subject {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Group"
    name      = local.runbook_group
  }
}

# A debug session reads the application namespace only, the targets.application of this
# layer.
resource "kubernetes_role_binding_v1" "debugger" {
  metadata {
    name      = "clanker-debugger"
    namespace = module.platform.app_namespace

    labels = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
    }
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = "view"
  }

  subject {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Group"
    name      = local.debugger_group
  }
}
