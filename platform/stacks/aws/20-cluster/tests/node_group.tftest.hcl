# The node group and its launch template in modules/aws/cluster. All ARNs, IDs and tag
# values are fakes.

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
  access_entries           = {}
}

run "launch_template_carries_the_instance_tags" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  variables {
    instance_tags = {
      test-required-key = "test-required-value"
      clanker_env       = "must-not-win"
    }
  }

  assert {
    condition = (
      toset([for spec in aws_launch_template.node.tag_specifications : spec.resource_type])
      == toset(["instance", "volume"])
    )
    error_message = "The launch template must tag the instances and the volumes."
  }

  assert {
    condition = alltrue([
      for spec in aws_launch_template.node.tag_specifications :
      spec.tags["test-required-key"] == "test-required-value"
    ])
    error_message = "Each tag specification must carry instance_tags."
  }

  assert {
    condition = alltrue([
      for spec in aws_launch_template.node.tag_specifications :
      spec.tags["clanker_env"] == "test-env" && spec.tags["clanker_layer"] == "20-cluster"
      && spec.tags["clanker_ttl"] == "168"
    ])
    error_message = "Each tag specification must carry the clanker_* tags, which win."
  }

  assert {
    condition = alltrue([
      for spec in aws_launch_template.node.tag_specifications :
      issensitive(spec.tags["test-required-key"]) && !issensitive(spec.tags["clanker_env"])
    ])
    error_message = "Only the values of instance_tags must be sensitive."
  }

  assert {
    condition     = aws_launch_template.node.block_device_mappings[0].ebs[0].volume_size == 20
    error_message = "The node disk size must be in the launch template."
  }

  assert {
    condition = (
      aws_eks_node_group.default.launch_template[0].id == aws_launch_template.node.id
      && aws_eks_node_group.default.ami_type == "AL2023_ARM_64_STANDARD"
      && aws_eks_node_group.default.instance_types == tolist(["t4g.medium"])
    )
    error_message = "The node group must use the template, the AMI type and the instance type."
  }
}

run "launch_template_tags_without_instance_tags" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  variables {
  }

  assert {
    condition = alltrue([
      for spec in aws_launch_template.node.tag_specifications : length(spec.tags) == 3
    ])
    error_message = "Without instance_tags the tags must be the three clanker_* tags."
  }
}

run "other_ami_types_are_refused" {
  command = plan

  module {
    source = "../../../modules/aws/cluster"
  }

  variables {
    ami_type = "BOTTLEROCKET_ARM_64"
  }

  expect_failures = [var.ami_type]
}
