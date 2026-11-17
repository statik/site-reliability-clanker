locals {
  layer     = "40-platform"
  bootstrap = data.terraform_remote_state.bootstrap.outputs
  cluster   = data.terraform_remote_state.cluster.outputs
  data      = data.terraform_remote_state.data.outputs

  eks_token_args = [
    "eks", "get-token", "--cluster-name", local.cluster.cluster_name, "--region", var.region,
  ]

  postgres_admin = jsondecode(data.aws_secretsmanager_secret_version.postgres_admin.secret_string)
}

data "terraform_remote_state" "bootstrap" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/00-bootstrap.tfstate"
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

data "terraform_remote_state" "data" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/30-data.tfstate"
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

provider "helm" {
  kubernetes = {
    host                   = local.cluster.cluster_endpoint
    cluster_ca_certificate = local.cluster.cluster_ca_certificate

    exec = {
      api_version = "client.authentication.k8s.io/v1beta1"
      command     = "aws"
      args        = local.eks_token_args
    }
  }
}

data "aws_secretsmanager_secret_version" "postgres_admin" {
  secret_id = local.data.postgres_admin_secret_ref
}

module "platform" {
  source = "../../../modules/shared/platform"

  env_name      = var.env_name
  app_namespace = var.app_namespace
  public_port   = var.public_port

  postgres_host           = local.data.postgres_host
  postgres_port           = local.data.postgres_port
  postgres_database       = local.data.postgres_database
  postgres_admin_password = local.postgres_admin.password
  rwx_storage_class       = local.data.rwx_storage_class
  shared_fs_capacity_gb   = local.data.shared_fs_capacity_gb

  # Envoy Gateway makes a Service of type LoadBalancer, and AWS gives that Service a load
  # balancer. Its DNS name does not exist before the apply, so the shared module finds it.
  gateway_class  = "envoy"
  public_address = ""

  # Without the first annotation the cloud controller in EKS makes a Classic Load Balancer.
  # The load balancer is internet-facing by default and goes into the public subnets.
  service_annotations = {
    "service.beta.kubernetes.io/aws-load-balancer-type" = "nlb"
    "service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags" = join(",", [
      "clanker_env=${var.env_name}",
      "clanker_layer=${local.layer}",
      "clanker_ttl=${local.bootstrap.clanker_ttl}",
    ])
  }
}
