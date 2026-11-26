# The root module of 20-cluster: the node defaults, the access entries, the CA output and
# the path of the runners to the API server. All ARNs, IDs and ranges are fakes.

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

  mock_data "aws_vpc" {
    defaults = {
      cidr_block_associations = [
        { association_id = "vpc-cidr-assoc-0000000a", cidr_block = "10.0.0.0/16", state = "associated" },
        { association_id = "vpc-cidr-assoc-0000000b", cidr_block = "100.70.0.0/16", state = "associated" },
      ]
    }
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
        runbook               = "arn:aws:iam::000000000000:role/clanker/test-runbook"
        "applier-30-data"     = "arn:aws:iam::000000000000:role/clanker/test-applier-30"
        "applier-40-platform" = "arn:aws:iam::000000000000:role/clanker/test-applier-40"
        "applier-50-app"      = "arn:aws:iam::000000000000:role/clanker/test-applier-50"
      }
    }
  }
}

override_data {
  target = data.terraform_remote_state.network
  values = {
    outputs = {
      network_id         = "vpc-0000000a"
      private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"]
    }
  }
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

run "runbook_has_only_its_kubernetes_group" {
  command = plan

  assert {
    condition = (
      local.access_entries["runbook"].principal_arn == "arn:aws:iam::000000000000:role/clanker/test-runbook"
      && local.access_entries["runbook"].kubernetes_groups == ["clanker-runbook"]
      && !contains(keys(local.access_entries["runbook"]), "policy")
    )
    error_message = "The runbook role must map to the group clanker-runbook with no access policy."
  }
}

run "debugger_has_only_its_kubernetes_group" {
  command = plan

  assert {
    condition = (
      local.access_entries["debugger"].principal_arn == "arn:aws:iam::000000000000:role/clanker/test-debugger"
      && local.access_entries["debugger"].kubernetes_groups == ["clanker-debugger"]
      && !contains(keys(local.access_entries["debugger"]), "policy")
    )
    error_message = "The debugger role must map to the group clanker-debugger with no access policy."
  }
}

run "runners_reach_the_api_from_the_network" {
  command = plan

  assert {
    condition = (
      toset(keys(aws_vpc_security_group_ingress_rule.api_from_network)) ==
      toset(["10.0.0.0/16", "100.70.0.0/16"])
    )
    error_message = "Each address range of the network must reach the API server."
  }

  assert {
    condition = alltrue([
      for rule in values(aws_vpc_security_group_ingress_rule.api_from_network) :
      rule.ip_protocol == "tcp" && rule.from_port == 443 && rule.to_port == 443
    ])
    error_message = "The rules must admit TCP port 443 only."
  }
}

run "the_ca_is_an_output_that_is_not_sensitive" {
  command = apply

  assert {
    condition     = output.cluster_certificate_authority_data == "dGVzdA=="
    error_message = "The CA must be in an output in base64, as EKS gives it."
  }
}
