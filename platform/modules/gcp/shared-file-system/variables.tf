variable "name" {
  description = "Name of the Filestore instance and of the PersistentVolume."
  type        = string
}

variable "zone" {
  description = "Zone of the instance."
  type        = string
}

variable "network_id" {
  description = "ID of the VPC network."
  type        = string
}

variable "private_services_range_name" {
  description = "Name of the range that is allocated for private services access."
  type        = string
}

variable "capacity_gb" {
  description = "Capacity of the file share in GiB."
  type        = number

  validation {
    condition     = var.capacity_gb >= 1024
    error_message = "capacity_gb must be 1024 or more. The Filestore BASIC_HDD tier has a 1 TiB minimum."
  }
}

variable "storage_class_name" {
  description = "Name of the StorageClass to create."
  type        = string
}
