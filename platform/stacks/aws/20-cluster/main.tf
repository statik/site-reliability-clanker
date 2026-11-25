locals {
  layer      = "20-cluster"
  bootstrap  = data.terraform_remote_state.bootstrap.outputs
  network    = data.terraform_remote_state.network.outputs
  identities = local.bootstrap.identities

  # The debugger works in the application namespace only. `paperboat` is the contract default
  # of `app_namespace` in 40-platform.
  debugger_namespaces = ["paperboat"]

  cluster_admin = "AmazonEKSClusterAdminPolicy"

  # The role that applies this layer creates the cluster, and EKS makes it a cluster admin.
  access_entries = {
    observer = {
      principal_arn = local.identities["observer"]
      policy        = "AmazonEKSViewPolicy"
      namespaces    = []
    }
    planner = {
      principal_arn = local.identities["planner"]
      policy        = "AmazonEKSAdminViewPolicy"
      namespaces    = []
    }
    debugger = {
      principal_arn = local.identities["debugger"]
      policy        = "AmazonEKSEditPolicy"
      namespaces    = local.debugger_namespaces
    }
    applier-30-data = {
      principal_arn = local.identities["applier-30-data"]
      policy        = local.cluster_admin
      namespaces    = []
    }
    applier-40-platform = {
      principal_arn = local.identities["applier-40-platform"]
      policy        = local.cluster_admin
      namespaces    = []
    }
    applier-50-app = {
      principal_arn = local.identities["applier-50-app"]
      policy        = local.cluster_admin
      namespaces    = []
    }
  }

  # The operator can remove the environment after the sessions of the agent roles end.
  operator_entry = var.operator_principal_arn == "" ? {} : {
    operator = {
      principal_arn = var.operator_principal_arn
      policy        = local.cluster_admin
      namespaces    = []
    }
  }
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

module "cluster" {
  source = "../../../modules/aws/cluster"

  name                     = "clanker-${var.env_name}"
  kubernetes_version       = var.kubernetes_version
  node_image_version       = var.node_image_version
  node_count               = var.node_count
  node_size                = var.node_size
  ami_type                 = var.node_ami_type
  instance_tags            = var.instance_tags
  subnet_ids               = local.network.private_subnet_ids
  role_path                = local.bootstrap.workload_role_path
  permissions_boundary_arn = local.bootstrap.permission_guard
  access_entries           = merge(local.access_entries, local.operator_entry)
}
