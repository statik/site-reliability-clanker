# The access entries of modules/aws/cluster: an entry with an access policy, and an entry with
# only Kubernetes groups. All ARNs and IDs are fakes.

mock_provider "aws" {
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
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

variables {
  name                     = "clanker-test-env"
  kubernetes_version       = "1.36"
  node_image_version       = ""
  node_count               = 2
  node_size                = "t4g.medium"
  ami_type                 = "AL2023_ARM_64_STANDARD"
  subnet_ids               = ["subnet-0000000a", "subnet-0000000b"]
  role_path                = "/clanker-workload/"
  permissions_boundary_arn = "arn:aws:iam::000000000000:policy/test-boundary"
  access_entries = {
    viewer = {
      principal_arn = "arn:aws:iam::000000000000:role/clanker/test-viewer"
      policy        = "AmazonEKSViewPolicy"
      namespaces    = ["test-namespace"]
    }
    grouped = {
      principal_arn     = "arn:aws:iam::000000000000:role/clanker/test-grouped"
      kubernetes_groups = ["test-group"]
    }
  }
}

run "an_entry_with_groups_has_no_access_policy" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  assert {
    condition = (
      toset(keys(aws_eks_access_entry.this)) == toset(["viewer", "grouped"])
      && keys(aws_eks_access_policy_association.this) == ["viewer"]
    )
    error_message = "Only the entry with a policy may get a policy association."
  }

  assert {
    condition = (
      aws_eks_access_entry.this["grouped"].kubernetes_groups == toset(["test-group"])
      && length(aws_eks_access_entry.this["viewer"].kubernetes_groups) == 0
    )
    error_message = "Only the entry with groups may name Kubernetes groups."
  }

  assert {
    condition = (
      aws_eks_access_policy_association.this["viewer"].access_scope[0].type == "namespace"
      && aws_eks_access_policy_association.this["viewer"].access_scope[0].namespaces == toset(["test-namespace"])
    )
    error_message = "The policy of an entry with namespaces must have namespace scope."
  }
}

run "an_entry_needs_a_policy_or_a_group" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  variables {
    access_entries = {
      empty = { principal_arn = "arn:aws:iam::000000000000:role/clanker/test-empty" }
    }
  }

  expect_failures = [var.access_entries]
}

run "namespaces_need_a_policy" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  variables {
    access_entries = {
      scoped = {
        principal_arn     = "arn:aws:iam::000000000000:role/clanker/test-scoped"
        kubernetes_groups = ["test-group"]
        namespaces        = ["test-namespace"]
      }
    }
  }

  expect_failures = [var.access_entries]
}
