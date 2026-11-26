locals {
  layer      = "20-cluster"
  bootstrap  = data.terraform_remote_state.bootstrap.outputs
  network    = data.terraform_remote_state.network.outputs
  identities = local.bootstrap.identities

  cluster_admin = "AmazonEKSClusterAdminPolicy"

  # The Kubernetes groups of the debugger and runbook roles. They have no access policy:
  # 40-platform binds each group to the rights of the identity in the application namespace,
  # which this layer does not know.
  debugger_group = "clanker-debugger"
  runbook_group  = "clanker-runbook"

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
      principal_arn     = local.identities["debugger"]
      kubernetes_groups = [local.debugger_group]
    }
    runbook = {
      principal_arn     = local.identities["runbook"]
      kubernetes_groups = [local.runbook_group]
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

# The runners of the control plane run in the network of the environment. There the name of
# the API server resolves to its private endpoint, and the security group of the cluster
# admits only the nodes. This rule admits the address ranges of the network on port 443. The
# endpoint is public as well, so the rule opens no path from outside the network.
data "aws_vpc" "network" {
  id = local.network.network_id
}

resource "aws_vpc_security_group_ingress_rule" "api_from_network" {
  for_each = toset(data.aws_vpc.network.cidr_block_associations[*].cidr_block)

  security_group_id = module.cluster.node_security_group_id
  description       = "Kubernetes API from the network, for the runners of the control plane"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = each.value
}
