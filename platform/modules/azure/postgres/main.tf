terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
    random = {
      source = "hashicorp/random"
    }
  }
}

variable "name" {
  type        = string
  description = "Globally unique server name."
}

variable "resource_group_name" {
  type        = string
  description = "Workload resource group."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "major_version" {
  type        = string
  description = "PostgreSQL major version, for example 17."
}

variable "storage_gb" {
  type        = number
  description = "Storage in GiB. Azure accepts 32, 64, 128 and larger powers of two."
}

variable "backup_retention_days" {
  type        = number
  description = "Retention for automatic backups, 7 to 35 days."
}

variable "zone" {
  type        = string
  description = "Availability zone of the server."
}

variable "delegated_subnet_id" {
  type        = string
  description = "Subnet delegated to Microsoft.DBforPostgreSQL/flexibleServers."
}

variable "private_dns_zone_id" {
  type        = string
  description = "Private DNS zone that is linked to the virtual network."
}

variable "key_vault_id" {
  type        = string
  description = "Key Vault that stores the administrator password."
}

variable "database_name" {
  type        = string
  description = "Database for the application."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

# The shared platform module receives a password but no user name, so the administrator
# has the conventional name.
locals {
  administrator_login = "postgres"
  admin_secret_name   = "postgres-admin-password"
}

# No special characters, so that the password needs no escaping in DATABASE_URL.
resource "random_password" "admin" {
  length  = 32
  special = false
}

resource "azurerm_key_vault_secret" "admin" {
  name         = local.admin_secret_name
  key_vault_id = var.key_vault_id
  value        = random_password.admin.result
  content_type = "PostgreSQL password of ${local.administrator_login}"
  tags         = var.tags
}

resource "azurerm_postgresql_flexible_server" "main" {
  name                          = var.name
  resource_group_name           = var.resource_group_name
  location                      = var.location
  version                       = var.major_version
  sku_name                      = "B_Standard_B1ms"
  storage_mb                    = var.storage_gb * 1024
  zone                          = var.zone
  delegated_subnet_id           = var.delegated_subnet_id
  private_dns_zone_id           = var.private_dns_zone_id
  public_network_access_enabled = false
  backup_retention_days         = var.backup_retention_days
  geo_redundant_backup_enabled  = false
  administrator_login           = local.administrator_login
  administrator_password        = random_password.admin.result
  tags                          = var.tags

  authentication {
    password_auth_enabled         = true
    active_directory_auth_enabled = false
  }
}

resource "azurerm_postgresql_flexible_server_database" "app" {
  name      = var.database_name
  server_id = azurerm_postgresql_flexible_server.main.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

output "host" {
  value = azurerm_postgresql_flexible_server.main.fqdn
}

output "database" {
  value = azurerm_postgresql_flexible_server_database.app.name
}

output "major_version" {
  value       = azurerm_postgresql_flexible_server.main.version
  description = "Major version that runs now. Azure does not report the minor version here."
}

output "administrator_login" {
  value = local.administrator_login
}

output "admin_password" {
  value     = random_password.admin.result
  sensitive = true
}

output "admin_secret_ref" {
  value       = azurerm_key_vault_secret.admin.versionless_id
  description = "Key Vault URL of the secret, without a version."
}
