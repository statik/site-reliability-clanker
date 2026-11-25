# The root module of 20-cluster: the node defaults and the operator access entry. All ARNs
# and IDs are fakes.

mock_provider "aws" {
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }

  mock_data "aws_default_tags" {
    defaults = {
      tags = { clanker_env = "test-env", clanker_layer = "20-cluster", clanker_ttl = "168" }
    }
  }

  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }

  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::000000000000:role/clanker-workload/test-role" }
  }

  mock_resource "aws_eks_cluster" {
    defaults = {
      certificate_authority = [{ data = "dGVzdA==" }]
    }
  }

  mock_resource "aws_launch_template" {
    defaults = { id = "lt-00000000", latest_version = 1 }
  }
}

override_data {
  target = data.terraform_remote_state.bootstrap
  values = {
    outputs = {
      clanker_ttl        = "168"
      workload_role_path = "/clanker-workload/"
      permission_guard   = "arn:aws:iam::000000000000:policy/test-boundary"
      identities = {
        observer              = "arn:aws:iam::000000000000:role/clanker/test-observer"
        planner               = "arn:aws:iam::000000000000:role/clanker/test-planner"
        debugger              = "arn:aws:iam::000000000000:role/clanker/test-debugger"
        "applier-30-data"     = "arn:aws:iam::000000000000:role/clanker/test-applier-30"
        "applier-40-platform" = "arn:aws:iam::000000000000:role/clanker/test-applier-40"
        "applier-50-app"      = "arn:aws:iam::000000000000:role/clanker/test-applier-50"
      }
    }
  }
}

override_data {
  target = data.terraform_remote_state.network
  values = { outputs = { private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"] } }
}

variables {
  env_name           = "test-env"
  state_location     = "test-state"
  region             = "us-east-1"
  kubernetes_version = "1.36"
  node_image_version = ""
  node_count         = 2
}

run "graviton_nodes_by_default" {
  command = plan

  assert {
    condition     = var.node_ami_type == "AL2023_ARM_64_STANDARD" && var.node_size == "t4g.medium"
    error_message = "The defaults must be Graviton nodes: AL2023_ARM_64_STANDARD on t4g.medium."
  }
}

run "no_operator_entry_without_an_operator" {
  command = plan

  assert {
    condition     = length(local.operator_entry) == 0
    error_message = "An empty operator_principal_arn must not make an access entry."
  }
}

run "operator_is_a_cluster_admin" {
  command = plan

  variables {
    operator_principal_arn = "arn:aws:iam::000000000000:role/test-path/test-operator"
  }

  assert {
    condition = (
      local.operator_entry["operator"].principal_arn == var.operator_principal_arn
      && local.operator_entry["operator"].policy == "AmazonEKSClusterAdminPolicy"
      && length(local.operator_entry["operator"].namespaces) == 0
    )
    error_message = "The operator must get a cluster-wide AmazonEKSClusterAdminPolicy entry."
  }
}

run "operator_session_arn_is_refused" {
  command = plan

  variables {
    operator_principal_arn = "arn:aws:sts::000000000000:assumed-role/test-operator/test-session"
  }

  expect_failures = [var.operator_principal_arn]
}
