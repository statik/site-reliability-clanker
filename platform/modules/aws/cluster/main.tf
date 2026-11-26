locals {
  partition            = data.aws_partition.current.partition
  access_policy_prefix = "arn:${local.partition}:eks::aws:cluster-access-policy"
}

resource "aws_eks_cluster" "this" {
  name     = var.name
  version  = var.kubernetes_version
  role_arn = aws_iam_role.this["cluster"].arn

  access_config {
    authentication_mode                         = "API"
    bootstrap_cluster_creator_admin_permissions = true
  }

  vpc_config {
    subnet_ids              = var.subnet_ids
    endpoint_private_access = true
    endpoint_public_access  = true
  }

  upgrade_policy {
    support_type = "STANDARD"
  }

  depends_on = [aws_iam_role_policy_attachment.this]
}

# Provider default tags do not reach tag_specifications, so the clanker_* tags are read
# here and win over an extra tag with the same key.
data "aws_default_tags" "current" {}

locals {
  # Each value of instance_tags is marked sensitive. The plan then marks it, and the
  # control plane gives every marked value to its scrubber before a model reads the plan.
  # The clanker_* values stay readable.
  launch_tags = merge(
    { for key, value in var.instance_tags : key => sensitive(value) },
    data.aws_default_tags.current.tags,
  )
}

# A managed node group without a launch template puts no tags on its instances. With this
# template, each RunInstances call of the node group carries the tags.
resource "aws_launch_template" "node" {
  name_prefix            = "${var.name}-node-"
  update_default_version = true

  # With a launch template, EKS takes the disk size from the template only.
  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      volume_size           = 20
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }

  # IMDSv2 only. The EKS guide for launch templates asks for a hop limit of 2, so that a
  # container that uses IMDSv2 reaches it.
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  dynamic "tag_specifications" {
    for_each = toset(["instance", "volume"])

    content {
      resource_type = tag_specifications.value
      tags          = local.launch_tags
    }
  }
}

resource "aws_eks_node_group" "default" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "default"
  node_role_arn   = aws_iam_role.this["node"].arn
  subnet_ids      = var.subnet_ids

  version         = aws_eks_cluster.this.version
  release_version = var.node_image_version == "" ? null : var.node_image_version
  ami_type        = var.ami_type
  capacity_type   = "ON_DEMAND"
  instance_types  = [var.node_size]

  launch_template {
    id      = aws_launch_template.node.id
    version = aws_launch_template.node.latest_version
  }

  scaling_config {
    desired_size = var.node_count
    min_size     = var.node_count
    max_size     = var.node_count
  }

  update_config {
    max_unavailable = 1
  }

  depends_on = [aws_iam_role_policy_attachment.this]
}

resource "aws_eks_addon" "pod_identity_agent" {
  cluster_name = aws_eks_cluster.this.name
  addon_name   = "eks-pod-identity-agent"

  depends_on = [aws_eks_node_group.default]
}

resource "aws_eks_addon" "efs_csi_driver" {
  cluster_name = aws_eks_cluster.this.name
  addon_name   = "aws-efs-csi-driver"

  pod_identity_association {
    role_arn        = aws_iam_role.this["efs-csi"].arn
    service_account = "efs-csi-controller-sa"
  }

  depends_on = [aws_eks_addon.pod_identity_agent]
}

resource "aws_eks_access_entry" "this" {
  for_each = var.access_entries

  cluster_name  = aws_eks_cluster.this.name
  principal_arn = each.value.principal_arn
  type          = "STANDARD"
  kubernetes_groups = (
    length(each.value.kubernetes_groups) == 0 ? null : each.value.kubernetes_groups
  )
}

# An entry with only Kubernetes groups gets its rights from role bindings in the cluster.
resource "aws_eks_access_policy_association" "this" {
  for_each = { for key, entry in var.access_entries : key => entry if entry.policy != "" }

  cluster_name  = aws_eks_cluster.this.name
  principal_arn = aws_eks_access_entry.this[each.key].principal_arn
  policy_arn    = "${local.access_policy_prefix}/${each.value.policy}"

  access_scope {
    type       = length(each.value.namespaces) == 0 ? "cluster" : "namespace"
    namespaces = length(each.value.namespaces) == 0 ? null : each.value.namespaces
  }
}
