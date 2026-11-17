locals {
  # The first quarter of network_cidr holds three equal parts. The second quarter is for pods.
  nodes_cidr            = cidrsubnet(var.network_cidr, 4, 0)
  services_cidr         = cidrsubnet(var.network_cidr, 4, 1)
  private_services_cidr = cidrsubnet(var.network_cidr, 4, 2)
  pods_cidr             = cidrsubnet(var.network_cidr, 2, 1)

  pods_range_name     = "pods"
  services_range_name = "services"
}

data "google_compute_zones" "available" {
  region = var.region
  status = "UP"
}

resource "google_compute_network" "this" {
  name                    = var.name
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"
}

resource "google_compute_subnetwork" "private" {
  name                     = "${var.name}-private"
  region                   = var.region
  network                  = google_compute_network.this.id
  ip_cidr_range            = local.nodes_cidr
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = local.pods_range_name
    ip_cidr_range = local.pods_cidr
  }

  secondary_ip_range {
    range_name    = local.services_range_name
    ip_cidr_range = local.services_cidr
  }
}

resource "google_compute_router" "this" {
  name    = "${var.name}-nat"
  region  = var.region
  network = google_compute_network.this.id
}

resource "google_compute_router_nat" "this" {
  name                               = "${var.name}-nat"
  region                             = var.region
  router                             = google_compute_router.this.name
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# Private services access. Cloud SQL and Filestore take their addresses from this range.
resource "google_compute_global_address" "private_services" {
  name          = "${var.name}-private-services"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  network       = google_compute_network.this.id
  address       = cidrhost(local.private_services_cidr, 0)
  prefix_length = tonumber(split("/", local.private_services_cidr)[1])
}

resource "google_service_networking_connection" "private_services" {
  network                 = google_compute_network.this.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_services.name]
}

# The load balancer of the gateway in layer 40-platform uses this address.
resource "google_compute_address" "public" {
  name         = "${var.name}-public"
  region       = var.region
  address_type = "EXTERNAL"
  network_tier = "PREMIUM"
}
