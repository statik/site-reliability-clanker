locals {
  state_prefix_arn   = "${var.state_bucket_arn}/${var.env_name}"
  rds_secret_arn     = "arn:${local.partition}:secretsmanager:*:${local.account_id}:secret:rds!*"
  workload_role_arn  = "${local.iam_arn_prefix}:role${local.workload_role_path}*"
  service_linked_arn = "${local.iam_arn_prefix}:role/aws-service-role/*"
  security_group_edit = [
    "ec2:AuthorizeSecurityGroupEgress",
    "ec2:AuthorizeSecurityGroupIngress",
    "ec2:CreateSecurityGroup",
    "ec2:CreateTags",
    "ec2:DeleteSecurityGroup",
    "ec2:DeleteTags",
    "ec2:Describe*",
    "ec2:ModifySecurityGroupRules",
    "ec2:RevokeSecurityGroupEgress",
    "ec2:RevokeSecurityGroupIngress",
  ]
}

data "aws_iam_policy_document" "observer" {
  statement {
    sid    = "InventoryWithoutSecretValues"
    effect = "Allow"
    actions = [
      "ecr:Describe*",
      "ecr:List*",
      "eks:Describe*",
      "eks:List*",
      "elasticfilesystem:Describe*",
      "rds:Describe*",
    ]
    resources = ["*"]
  }
}

data "aws_iam_policy_document" "planner" {
  statement {
    sid       = "StateLock"
    effect    = "Allow"
    actions   = ["s3:PutObject", "s3:DeleteObject"]
    resources = ["${local.state_prefix_arn}/*.tflock"]
  }

  statement {
    sid       = "DatabaseAdminSecretForThePlatformLayerPlan"
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [local.rds_secret_arn]
  }
}

data "aws_iam_policy_document" "debugger" {
  statement {
    sid       = "FindTheCluster"
    effect    = "Allow"
    actions   = ["eks:DescribeCluster", "eks:ListClusters"]
    resources = ["*"]
  }
}

data "aws_iam_policy_document" "applier_state" {
  for_each = toset(var.applier_layers)

  statement {
    sid       = "ListState"
    effect    = "Allow"
    actions   = ["s3:ListBucket"]
    resources = [var.state_bucket_arn]
  }

  statement {
    sid       = "ReadStateOfAllLayers"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["${local.state_prefix_arn}/*"]
  }

  statement {
    sid     = "WriteStateOfThisLayer"
    effect  = "Allow"
    actions = ["s3:PutObject", "s3:DeleteObject"]
    resources = [
      "${local.state_prefix_arn}/${each.key}.tfstate",
      "${local.state_prefix_arn}/${each.key}.tfstate.tflock",
    ]
  }
}

data "aws_iam_policy_document" "layer_10_network" {
  statement {
    sid       = "NetworkAndRegistry"
    effect    = "Allow"
    actions   = ["ec2:*", "ecr:*"]
    resources = ["*"]
  }
}

data "aws_iam_policy_document" "layer_20_cluster" {
  statement {
    sid    = "ClusterAndNodes"
    effect = "Allow"
    actions = [
      "autoscaling:*",
      "ec2:*",
      "eks:*",
      "iam:GetPolicy",
      "iam:GetPolicyVersion",
      "kms:DescribeKey",
      "logs:*",
      "ssm:GetParameter*",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "RolesForTheClusterNodesAndPods"
    effect = "Allow"
    actions = [
      "iam:AttachRolePolicy",
      "iam:CreateRole",
      "iam:DeleteRole",
      "iam:DetachRolePolicy",
      "iam:GetRole",
      "iam:ListAttachedRolePolicies",
      "iam:ListInstanceProfilesForRole",
      "iam:ListRolePolicies",
      "iam:PassRole",
      "iam:TagRole",
      "iam:UntagRole",
      "iam:UpdateAssumeRolePolicy",
    ]
    resources = [local.workload_role_arn]
  }

  statement {
    sid       = "ServiceLinkedRoles"
    effect    = "Allow"
    actions   = ["iam:CreateServiceLinkedRole"]
    resources = [local.service_linked_arn]

    condition {
      test     = "StringEquals"
      variable = "iam:AWSServiceName"
      values = [
        "autoscaling.amazonaws.com",
        "eks.amazonaws.com",
        "eks-nodegroup.amazonaws.com",
      ]
    }
  }
}

data "aws_iam_policy_document" "layer_30_data" {
  statement {
    sid    = "DatabaseFileSystemBucketsAndSecrets"
    effect = "Allow"
    actions = concat(local.security_group_edit, [
      "ec2:CreateNetworkInterface",
      "ec2:DeleteNetworkInterface",
      "ec2:ModifyNetworkInterfaceAttribute",
      "elasticfilesystem:*",
      "kms:CreateGrant",
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:GenerateDataKey",
      "rds:*",
      "s3:*",
      "secretsmanager:*",
    ])
    resources = ["*"]
  }

  statement {
    sid       = "ServiceLinkedRoles"
    effect    = "Allow"
    actions   = ["iam:CreateServiceLinkedRole"]
    resources = [local.service_linked_arn]

    condition {
      test     = "StringEquals"
      variable = "iam:AWSServiceName"
      values   = ["elasticfilesystem.amazonaws.com", "rds.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "layer_40_platform" {
  statement {
    sid       = "DatabaseAdminSecret"
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [local.rds_secret_arn]
  }

  statement {
    sid       = "ReadTheLoadBalancer"
    effect    = "Allow"
    actions   = ["elasticloadbalancing:Describe*"]
    resources = ["*"]
  }
}

data "aws_iam_policy_document" "layer_50_app" {
  statement {
    sid       = "FindTheCluster"
    effect    = "Allow"
    actions   = ["eks:DescribeCluster"]
    resources = ["*"]
  }
}

locals {
  layer_policy_json = {
    "10-network"  = data.aws_iam_policy_document.layer_10_network.json
    "20-cluster"  = data.aws_iam_policy_document.layer_20_cluster.json
    "30-data"     = data.aws_iam_policy_document.layer_30_data.json
    "40-platform" = data.aws_iam_policy_document.layer_40_platform.json
    "50-app"      = data.aws_iam_policy_document.layer_50_app.json
  }
}

data "aws_iam_policy_document" "applier" {
  for_each = toset(var.applier_layers)

  source_policy_documents = [
    data.aws_iam_policy_document.applier_state[each.key].json,
    local.layer_policy_json[each.key],
  ]
}
