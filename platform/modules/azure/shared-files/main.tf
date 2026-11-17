terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
    kubernetes = {
      source = "hashicorp/kubernetes"
    }
  }
}

variable "storage_account_name" {
  type        = string
  description = "Globally unique storage account name."
}

variable "resource_group_name" {
  type        = string
  description = "Workload resource group."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "capacity_gb" {
  type        = number
  description = "Provisioned size of the NFS share in GiB. The minimum is 100."
}

variable "private_endpoints_subnet_id" {
  type        = string
  description = "Subnet for the private endpoint of the storage account."
}

variable "private_dns_zone_id" {
  type        = string
  description = "ID of the privatelink.file.core.windows.net zone."
}

variable "storage_class_name" {
  type        = string
  description = "Name of the ReadWriteMany StorageClass."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

# NFS shares need a premium FileStorage account, no HTTPS-only rule and a private network path.
resource "azurerm_storage_account" "files" {
  name                       = var.storage_account_name
  resource_group_name        = var.resource_group_name
  location                   = var.location
  account_kind               = "FileStorage"
  account_tier               = "Premium"
  account_replication_type   = "LRS"
  https_traffic_only_enabled = false
  public_network_access      = "Disabled"
  tags                       = var.tags
}

resource "azurerm_storage_share" "shared" {
  name               = "shared"
  storage_account_id = azurerm_storage_account.files.id
  enabled_protocol   = "NFS"
  quota              = var.capacity_gb
}

resource "azurerm_private_endpoint" "files" {
  name                = "pe-${var.storage_account_name}-file"
  resource_group_name = var.resource_group_name
  location            = var.location
  subnet_id           = var.private_endpoints_subnet_id
  tags                = var.tags

  private_service_connection {
    name                           = "file"
    private_connection_resource_id = azurerm_storage_account.files.id
    subresource_names              = ["file"]
    is_manual_connection           = false
  }

  private_dns_zone_group {
    name                 = "file"
    private_dns_zone_ids = [var.private_dns_zone_id]
  }
}

# Each claim of this class mounts the one share above. The policy is Retain, so that the
# deletion of a claim does not delete the share.
resource "kubernetes_storage_class_v1" "rwx" {
  metadata {
    name = var.storage_class_name
  }

  storage_provisioner    = "file.csi.azure.com"
  reclaim_policy         = "Retain"
  volume_binding_mode    = "Immediate"
  allow_volume_expansion = true
  mount_options          = ["nconnect=4", "noresvport", "actimeo=30"]

  parameters = {
    protocol       = "nfs"
    skuName        = "Premium_LRS"
    resourceGroup  = var.resource_group_name
    storageAccount = azurerm_storage_account.files.name
    shareName      = azurerm_storage_share.shared.name
  }

  depends_on = [azurerm_private_endpoint.files]
}

output "storage_class_name" {
  value = kubernetes_storage_class_v1.rwx.metadata[0].name
}

output "storage_account_name" {
  value = azurerm_storage_account.files.name
}

output "share_name" {
  value = azurerm_storage_share.shared.name
}
