terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

variable "name" {
  type        = string
  description = "Globally unique vault name, 3 to 24 characters."
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
  description = "Entra tenant that authorises access to the vault."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

# Access is by Azure RBAC only. The runner is outside the virtual network, so the public
# endpoint stays open.
resource "azurerm_key_vault" "main" {
  name                       = var.name
  resource_group_name        = var.resource_group_name
  location                   = var.location
  tenant_id                  = var.tenant_id
  sku_name                   = "standard"
  rbac_authorization_enabled = true
  purge_protection_enabled   = false
  soft_delete_retention_days = 7
  tags                       = var.tags
}

output "key_vault_id" {
  value = azurerm_key_vault.main.id
}

output "vault_uri" {
  value = azurerm_key_vault.main.vault_uri
}
