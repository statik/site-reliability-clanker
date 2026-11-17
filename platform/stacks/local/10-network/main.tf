provider "docker" {}

locals {
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "10-network"
    clanker_ttl   = "none"
  }
}

resource "docker_network" "this" {
  name   = "clanker-${var.env_name}"
  driver = "bridge"

  ipam_config {
    subnet = var.network_cidr
  }

  dynamic "labels" {
    for_each = local.labels
    content {
      label = labels.key
      value = labels.value
    }
  }
}

resource "docker_image" "registry" {
  name         = "registry:3.1.2"
  keep_locally = true
}

resource "docker_container" "registry" {
  name    = "clanker-${var.env_name}-registry"
  image   = docker_image.registry.image_id
  restart = "unless-stopped"

  ports {
    internal = 5000
    external = var.registry_port
    ip       = "127.0.0.1"
  }

  networks_advanced {
    name = docker_network.this.name
  }

  dynamic "labels" {
    for_each = local.labels
    content {
      label = labels.key
      value = labels.value
    }
  }
}
