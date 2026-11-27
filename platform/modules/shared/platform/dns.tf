# The host names of the applications in the cluster. On the local target the public DNS
# answers every name under app_domain with 127.0.0.1: the browser and the control plane
# reach the gateway there, and a pod reaches itself. So CoreDNS answers those names in the
# cluster with the Service of Envoy, whose port is public_port too, and one URL works from
# the host and from a pod.
#
# The layer writes the whole Corefile: the one of kubeadm with one rewrite. The destroy of
# the layer takes the key Corefile out of the ConfigMap; on the local target the destroy of
# 20-cluster then deletes the cluster.
locals {
  corefile = templatefile("${path.module}/coredns.Corefile.tftpl", {
    domain_pattern = replace(var.app_domain, ".", "\\.")
    envoy_name     = "${local.envoy_service_name}.${local.envoy_namespace}.svc.cluster.local"
  })
}

resource "kubernetes_config_map_v1_data" "coredns" {
  count = var.rewrite_cluster_dns ? 1 : 0

  metadata {
    name      = "coredns"
    namespace = "kube-system"
  }

  # kubeadm made the ConfigMap, so the layer takes the key over.
  force = true

  data = {
    Corefile = local.corefile
  }

  lifecycle {
    precondition {
      condition     = var.app_domain != ""
      error_message = "rewrite_cluster_dns needs app_domain: set the domain whose names CoreDNS answers with the Service of Envoy."
    }
  }
}
