provider "kind" {}

data "terraform_remote_state" "network" {
  backend = "local"
  config  = { path = "${var.state_location}/10-network.tfstate" }
}

locals {
  node_images = {
    "1.35" = "v1.35.8"
    "1.36" = "v1.36.4"
    "1.37" = "v1.37.0"
  }
  node_image_tag = coalesce(var.node_image_version, local.node_images[var.kubernetes_version])
  registry_host  = data.terraform_remote_state.network.outputs.image_registry
  registry_inner = data.terraform_remote_state.network.outputs.registry_internal_host
  certs_dir      = "${var.state_location}/certs.d"
}

resource "local_file" "registry_hosts" {
  filename = "${local.certs_dir}/${local.registry_host}/hosts.toml"
  content  = <<-EOT
    [host."http://${local.registry_inner}"]
      capabilities = ["pull", "resolve"]
  EOT
}

resource "kind_cluster" "this" {
  name            = "clanker-${var.env_name}"
  node_image      = "kindest/node:${local.node_image_tag}"
  kubeconfig_path = "${var.state_location}/kubeconfig"
  wait_for_ready  = true

  kind_config {
    kind        = "Cluster"
    api_version = "kind.x-k8s.io/v1alpha4"

    containerd_config_patches = [
      <<-EOT
      [plugins."io.containerd.cri.v1.images".registry]
        config_path = "/etc/containerd/certs.d"
      EOT
    ]

    node {
      role = "control-plane"

      extra_port_mappings {
        container_port = 30080
        host_port      = var.public_host_port
        listen_address = "127.0.0.1"
      }

      extra_mounts {
        host_path      = local.certs_dir
        container_path = "/etc/containerd/certs.d"
        read_only      = true
      }
    }

    dynamic "node" {
      for_each = range(var.node_count)
      content {
        role = "worker"

        extra_mounts {
          host_path      = local.certs_dir
          container_path = "/etc/containerd/certs.d"
          read_only      = true
        }
      }
    }
  }

  depends_on = [local_file.registry_hosts]
}
