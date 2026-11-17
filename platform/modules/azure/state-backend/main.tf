terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

variable "resource_group_name" {
  type        = string
  description = "Resource group for the storage account."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "storage_account_name" {
  type        = string
  description = "Globally unique storage account name."
}

variable "state_container_name" {
  type        = string
  description = "Blob container for OpenTofu state."
}

variable "evidence_container_name" {
  type        = string
  description = "Blob container for audit evidence."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

resource "azurerm_storage_account" "state" {
  name                            = var.storage_account_name
  resource_group_name             = var.resource_group_name
  location                        = var.location
  account_kind                    = "StorageV2"
  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  min_tls_version                 = "TLS1_2"
  https_traffic_only_enabled      = true
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = false
  default_to_oauth_authentication = true
  tags                            = var.tags

  blob_properties {
    versioning_enabled = true

    delete_retention_policy {
      days = 7
    }

    container_delete_retention_policy {
      days = 7
    }
  }
}

resource "azurerm_storage_container" "state" {
  name                  = var.state_container_name
  storage_account_id    = azurerm_storage_account.state.id
  container_access_type = "private"
}

resource "azurerm_storage_container" "evidence" {
  name                  = var.evidence_container_name
  storage_account_id    = azurerm_storage_account.state.id
  container_access_type = "private"
}

output "storage_account_name" {
  value = azurerm_storage_account.state.name
}

output "state_container_id" {
  value       = "${azurerm_storage_account.state.id}/blobServices/default/containers/${azurerm_storage_container.state.name}"
  description = "Resource Manager ID of the state container. Role assignments use it as scope."
}

output "state_container_name" {
  value = azurerm_storage_container.state.name
}

output "evidence_container_name" {
  value = azurerm_storage_container.evidence.name
}
