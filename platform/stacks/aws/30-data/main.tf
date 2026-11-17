locals {
  layer     = "30-data"
  name      = "clanker-${var.env_name}"
  bootstrap = data.terraform_remote_state.bootstrap.outputs
  network   = data.terraform_remote_state.network.outputs
  cluster   = data.terraform_remote_state.cluster.outputs

  eks_token_args = [
    "eks", "get-token", "--cluster-name", local.cluster.cluster_name, "--region", var.region,
  ]
}

data "terraform_remote_state" "bootstrap" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/00-bootstrap.tfstate"
    region = var.region
  }
}

data "terraform_remote_state" "network" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/10-network.tfstate"
    region = var.region
  }
}

data "terraform_remote_state" "cluster" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/20-cluster.tfstate"
    region = var.region
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
      clanker_ttl   = local.bootstrap.clanker_ttl
    }
  }
}

provider "kubernetes" {
  host                   = local.cluster.cluster_endpoint
  cluster_ca_certificate = local.cluster.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = local.eks_token_args
  }
}

data "aws_caller_identity" "current" {}

module "postgres" {
  source = "../../../modules/aws/postgres"

  name                     = local.name
  engine_version           = var.postgres_version
  storage_gb               = var.postgres_storage_gb
  backup_retention_days    = var.backup_retention_days
  database_name            = "paperboat"
  admin_username           = "postgres"
  vpc_id                   = local.network.network_id
  subnet_ids               = local.network.private_subnet_ids
  client_security_group_id = local.cluster.node_security_group_id
}

module "shared_fs" {
  source = "../../../modules/aws/shared-fs"

  name                     = local.name
  vpc_id                   = local.network.network_id
  subnet_ids               = local.network.private_subnet_ids
  client_security_group_id = local.cluster.node_security_group_id
}

resource "kubernetes_storage_class_v1" "rwx" {
  metadata {
    name = "efs-rwx"

    labels = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
    }
  }

  storage_provisioner = "efs.csi.aws.com"
  reclaim_policy      = "Delete"
  volume_binding_mode = "Immediate"

  parameters = {
    provisioningMode = "efs-ap"
    fileSystemId     = module.shared_fs.file_system_id
    directoryPerms   = "770"
    basePath         = "/dynamic"
  }
}

module "backup_bucket" {
  source = "../../../modules/aws/bucket"

  name = "${local.name}-backup-${data.aws_caller_identity.current.account_id}"
}
