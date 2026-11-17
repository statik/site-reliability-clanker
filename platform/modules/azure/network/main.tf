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

variable "network_cidr" {
  type        = string
  description = "Address range of the virtual network, /20 or larger."
}

variable "zone_count" {
  type        = number
  description = "Number of availability zones to use."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

locals {
  zones = [for index in range(var.zone_count) : tostring(index + 1)]

  # Sizes for a /16: nodes /18, the other subnets /24. A /20 gives nodes /22 and the others /28.
  subnet_cidrs = {
    nodes             = cidrsubnet(var.network_cidr, 2, 0)
    postgres          = cidrsubnet(var.network_cidr, 8, 64)
    private-endpoints = cidrsubnet(var.network_cidr, 8, 65)
  }

  private_dns_zones = {
    postgres = "${var.env_name}.private.postgres.database.azure.com"
    file     = "privatelink.file.core.windows.net"
  }
}

resource "azurerm_virtual_network" "main" {
  name                = "vnet-${var.env_name}"
  resource_group_name = var.resource_group_name
  location            = var.location
  address_space       = [var.network_cidr]
  tags                = var.tags
}

resource "azurerm_subnet" "nodes" {
  name                            = "snet-nodes"
  resource_group_name             = var.resource_group_name
  virtual_network_name            = azurerm_virtual_network.main.name
  address_prefixes                = [local.subnet_cidrs["nodes"]]
  default_outbound_access_enabled = false
}

resource "azurerm_subnet" "postgres" {
  name                            = "snet-postgres"
  resource_group_name             = var.resource_group_name
  virtual_network_name            = azurerm_virtual_network.main.name
  address_prefixes                = [local.subnet_cidrs["postgres"]]
  default_outbound_access_enabled = false

  delegation {
    name = "postgres-flexible-server"

    service_delegation {
      name    = "Microsoft.DBforPostgreSQL/flexibleServers"
      actions = ["Microsoft.Network/virtualNetworks/subnets/join/action"]
    }
  }
}

resource "azurerm_subnet" "private_endpoints" {
  name                              = "snet-private-endpoints"
  resource_group_name               = var.resource_group_name
  virtual_network_name              = azurerm_virtual_network.main.name
  address_prefixes                  = [local.subnet_cidrs["private-endpoints"]]
  default_outbound_access_enabled   = false
  private_endpoint_network_policies = "Disabled"
}

resource "azurerm_public_ip" "nat" {
  name                = "pip-${var.env_name}-nat"
  resource_group_name = var.resource_group_name
  location            = var.location
  allocation_method   = "Static"
  sku                 = "Standard"
  zones               = [local.zones[0]]
  tags                = var.tags
}

resource "azurerm_nat_gateway" "main" {
  name                    = "ng-${var.env_name}"
  resource_group_name     = var.resource_group_name
  location                = var.location
  sku_name                = "Standard"
  idle_timeout_in_minutes = 4
  zones                   = [local.zones[0]]
  tags                    = var.tags
}

resource "azurerm_nat_gateway_public_ip_association" "main" {
  nat_gateway_id       = azurerm_nat_gateway.main.id
  public_ip_address_id = azurerm_public_ip.nat.id
}

resource "azurerm_subnet_nat_gateway_association" "nodes" {
  subnet_id      = azurerm_subnet.nodes.id
  nat_gateway_id = azurerm_nat_gateway.main.id
}

resource "azurerm_private_dns_zone" "main" {
  for_each = local.private_dns_zones

  name                = each.value
  resource_group_name = var.resource_group_name
  tags                = var.tags
}

resource "azurerm_private_dns_zone_virtual_network_link" "main" {
  for_each = local.private_dns_zones

  name                = "vnet-${var.env_name}"
  private_dns_zone_id = azurerm_private_dns_zone.main[each.key].id
  virtual_network_id  = azurerm_virtual_network.main.id
  tags                = var.tags
}

output "network_id" {
  value = azurerm_virtual_network.main.id
}

output "subnet_ids" {
  value = {
    nodes             = azurerm_subnet.nodes.id
    postgres          = azurerm_subnet.postgres.id
    private-endpoints = azurerm_subnet.private_endpoints.id
  }
  description = "Subnet purpose to subnet ID."
}

output "private_dns_zone_ids" {
  value = {
    for key, zone in azurerm_private_dns_zone.main : key => zone.id
  }
  description = "Keys postgres and file. The virtual network links exist when these IDs are known."
  depends_on  = [azurerm_private_dns_zone_virtual_network_link.main]
}

output "zones" {
  value       = local.zones
  description = "Availability zones that the environment uses."
}
