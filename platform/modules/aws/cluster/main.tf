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

resource "aws_eks_node_group" "default" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "default"
  node_role_arn   = aws_iam_role.this["node"].arn
  subnet_ids      = var.subnet_ids

  version         = aws_eks_cluster.this.version
  release_version = var.node_image_version == "" ? null : var.node_image_version
  ami_type        = "AL2023_x86_64_STANDARD"
  capacity_type   = "ON_DEMAND"
  instance_types  = [var.node_size]
  disk_size       = 20

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
}

resource "aws_eks_access_policy_association" "this" {
  for_each = var.access_entries

  cluster_name  = aws_eks_cluster.this.name
  principal_arn = aws_eks_access_entry.this[each.key].principal_arn
  policy_arn    = "${local.access_policy_prefix}/${each.value.policy}"

  access_scope {
    type       = length(each.value.namespaces) == 0 ? "cluster" : "namespace"
    namespaces = length(each.value.namespaces) == 0 ? null : each.value.namespaces
  }
}
