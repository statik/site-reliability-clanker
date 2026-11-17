data "aws_partition" "current" {}

locals {
  managed_policy_prefix = "arn:${data.aws_partition.current.partition}:iam::aws:policy"

  service_trust = {
    cluster = { service = "eks.amazonaws.com", actions = ["sts:AssumeRole"] }
    node    = { service = "ec2.amazonaws.com", actions = ["sts:AssumeRole"] }
    efs-csi = {
      service = "pods.eks.amazonaws.com"
      actions = ["sts:AssumeRole", "sts:TagSession"]
    }
  }

  role_policies = {
    cluster-control-plane = { role = "cluster", policy = "AmazonEKSClusterPolicy" }
    node-worker           = { role = "node", policy = "AmazonEKSWorkerNodePolicy" }
    node-cni              = { role = "node", policy = "AmazonEKS_CNI_Policy" }
    node-image-pull       = { role = "node", policy = "AmazonEC2ContainerRegistryPullOnly" }
    efs-csi-driver = {
      role   = "efs-csi"
      policy = "service-role/AmazonEFSCSIDriverPolicy"
    }
  }
}

data "aws_iam_policy_document" "service_trust" {
  for_each = local.service_trust

  statement {
    effect  = "Allow"
    actions = each.value.actions

    principals {
      type        = "Service"
      identifiers = [each.value.service]
    }
  }
}

resource "aws_iam_role" "this" {
  for_each = local.service_trust

  name                 = "${var.name}-${each.key}"
  path                 = var.role_path
  assume_role_policy   = data.aws_iam_policy_document.service_trust[each.key].json
  permissions_boundary = var.permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "this" {
  for_each = local.role_policies

  role       = aws_iam_role.this[each.value.role].name
  policy_arn = "${local.managed_policy_prefix}/${each.value.policy}"
}
