terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
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

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

resource "azurerm_storage_account" "backups" {
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
}

resource "azurerm_storage_container" "backups" {
  name                  = "postgres-backups"
  storage_account_id    = azurerm_storage_account.backups.id
  container_access_type = "private"
}

output "backup_bucket" {
  value       = "${azurerm_storage_account.backups.name}/${azurerm_storage_container.backups.name}"
  description = "Backup location as <storage account>/<container>."
}
