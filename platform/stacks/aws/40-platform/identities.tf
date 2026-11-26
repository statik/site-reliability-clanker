# The rights of the runbook identity in Kubernetes: the rights that the local target gives
# it in the application namespace (internal/localtarget/identities-app.yaml). 20-cluster
# maps the runbook role to the group clanker-runbook with no access policy, so the group has
# only these rights. The data services are not in the cluster, so no data namespace gets a
# role.
locals {
  runbook_group = "clanker-runbook"
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
