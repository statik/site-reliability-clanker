terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

variable "env_name" {
  type        = string
  description = "Environment name. Resource names contain it."
}

variable "resource_group_name" {
  type        = string
  description = "Workload resource group."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "tenant_id" {
  type        = string
  description = "Entra tenant that authenticates Kubernetes users."
}

variable "kubernetes_version" {
  type        = string
  description = "Kubernetes minor version, for example 1.34."
}

variable "node_count" {
  type        = number
  description = "Number of nodes in the one node pool."
}

variable "node_size" {
  type        = string
  description = "VM size of the nodes."
}

variable "zones" {
  type        = list(string)
  description = "Availability zones for the nodes and the public address."
}

variable "nodes_subnet_id" {
  type        = string
  description = "Subnet for the nodes. It must have a NAT gateway."
}

variable "cluster_identity_id" {
  type        = string
  description = "User-assigned identity of the control plane."
}

variable "kubelet_identity" {
  type = object({
    id           = string
    client_id    = string
    principal_id = string
  })
  description = "User-assigned identity of the nodes."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

resource "azurerm_kubernetes_cluster" "main" {
  name                      = "aks-${var.env_name}"
  resource_group_name       = var.resource_group_name
  location                  = var.location
  dns_prefix                = var.env_name
  node_resource_group       = "rg-${var.env_name}-nodes"
  kubernetes_version        = var.kubernetes_version
  sku_tier                  = "Free"
  oidc_issuer_enabled       = true
  workload_identity_enabled = true
  local_account_disabled    = true
  node_os_upgrade_channel   = "None"
  tags                      = var.tags

  default_node_pool {
    name                        = "system"
    vm_size                     = var.node_size
    node_count                  = var.node_count
    orchestrator_version        = var.kubernetes_version
    vnet_subnet_id              = var.nodes_subnet_id
    zones                       = var.zones
    os_sku                      = "AzureLinux"
    os_disk_size_gb             = 32
    temporary_name_for_rotation = "systemtmp"
    tags                        = var.tags

    upgrade_settings {
      max_surge = "1"
    }
  }

  identity {
    type         = "UserAssigned"
    identity_ids = [var.cluster_identity_id]
  }

  kubelet_identity {
    client_id                 = var.kubelet_identity.client_id
    object_id                 = var.kubelet_identity.principal_id
    user_assigned_identity_id = var.kubelet_identity.id
  }

  azure_active_directory_role_based_access_control {
    tenant_id          = var.tenant_id
    azure_rbac_enabled = true
  }

  # The service and pod ranges are fixed. network_cidr must not overlap them.
  network_profile {
    network_plugin      = "azure"
    network_plugin_mode = "overlay"
    load_balancer_sku   = "standard"
    outbound_type       = "userAssignedNATGateway"
    service_cidr        = "172.16.0.0/16"
    dns_service_ip      = "172.16.0.10"
    pod_cidr            = "192.168.0.0/16"
  }

  # Manual means that the node pool below is the only source of nodes.
  node_provisioning_profile {
    mode = "Manual"
  }

  storage_profile {
    file_driver_enabled = true
  }
}

# The AKS cloud provider looks for a static address in the node resource group, unless the
# Service has an Azure annotation. The shared platform module must stay free of such
# annotations, so the address is here.
resource "azurerm_public_ip" "ingress" {
  name                = "pip-${var.env_name}-ingress"
  resource_group_name = azurerm_kubernetes_cluster.main.node_resource_group
  location            = var.location
  allocation_method   = "Static"
  sku                 = "Standard"
  zones               = var.zones
  tags                = var.tags
}

# The cluster resource does not report the node image, so the node pool is read back.
data "azurerm_kubernetes_cluster_node_pool" "system" {
  name                    = "system"
  kubernetes_cluster_name = azurerm_kubernetes_cluster.main.name
  resource_group_name     = azurerm_kubernetes_cluster.main.resource_group_name
}

output "cluster_name" {
  value = azurerm_kubernetes_cluster.main.name
}

output "cluster_id" {
  value = azurerm_kubernetes_cluster.main.id
}

output "cluster_endpoint" {
  value = azurerm_kubernetes_cluster.main.kube_config[0].host
}

output "cluster_ca_certificate" {
  value     = base64decode(azurerm_kubernetes_cluster.main.kube_config[0].cluster_ca_certificate)
  sensitive = true
}

output "kubernetes_version" {
  value = azurerm_kubernetes_cluster.main.current_kubernetes_version
}

output "node_image_version" {
  value = data.azurerm_kubernetes_cluster_node_pool.system.node_image_version
}

output "oidc_issuer_url" {
  value = azurerm_kubernetes_cluster.main.oidc_issuer_url
}

output "node_resource_group" {
  value = azurerm_kubernetes_cluster.main.node_resource_group
}

output "public_address" {
  value = azurerm_public_ip.ingress.ip_address
}
