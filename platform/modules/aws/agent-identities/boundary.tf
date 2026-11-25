locals {
  protected_bucket_arns = [var.state_bucket_arn, var.evidence_bucket_arn]

  # The ARN of the parameter /src/prod/X is parameter/src/prod/X. GetParametersByPath names
  # the path itself, so both the path and everything below it are in the list.
  parameter_arn_prefix = "arn:${local.partition}:ssm:*:${local.account_id}:parameter"
  core_parameter_arns = [
    "${local.parameter_arn_prefix}${var.core_parameter_path}",
    "${local.parameter_arn_prefix}${var.core_parameter_path}/*",
  ]
}

data "aws_iam_policy_document" "boundary" {
  statement {
    sid    = "ServicesThatThePlatformUses"
    effect = "Allow"
    actions = [
      "autoscaling:*",
      "cloudwatch:*",
      "ec2:*",
      "ecr:*",
      "eks:*",
      "eks-auth:*",
      "elasticfilesystem:*",
      "elasticloadbalancing:*",
      "iam:*",
      "kms:*",
      "logs:*",
      "rds:*",
      "s3:*",
      "secretsmanager:*",
      "ssm:GetParameter*",
      "sts:GetCallerIdentity",
      "tag:Get*",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "NoLongLivedIdentities"
    effect = "Deny"
    actions = [
      "iam:AddUserToGroup",
      "iam:AttachGroupPolicy",
      "iam:AttachUserPolicy",
      "iam:CreateAccessKey",
      "iam:CreateGroup",
      "iam:CreateLoginProfile",
      "iam:CreateOpenIDConnectProvider",
      "iam:CreateSAMLProvider",
      "iam:CreateUser",
      "iam:PutGroupPolicy",
      "iam:PutUserPolicy",
      "iam:UpdateAccessKey",
      "iam:UpdateLoginProfile",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "NewRolesMustCarryThisBoundary"
    effect = "Deny"
    actions = [
      "iam:AttachRolePolicy",
      "iam:CreateRole",
      "iam:DeleteRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PutRolePermissionsBoundary",
      "iam:PutRolePolicy",
    ]
    resources = ["*"]

    condition {
      test     = "StringNotEquals"
      variable = "iam:PermissionsBoundary"
      values   = [local.boundary_arn]
    }
  }

  statement {
    sid       = "NoBoundaryRemoval"
    effect    = "Deny"
    actions   = ["iam:DeleteRolePermissionsBoundary"]
    resources = ["*"]
  }

  statement {
    sid    = "NoBoundaryEdit"
    effect = "Deny"
    actions = [
      "iam:CreatePolicyVersion",
      "iam:DeletePolicy",
      "iam:DeletePolicyVersion",
      "iam:SetDefaultPolicyVersion",
    ]
    resources = [local.boundary_arn]
  }

  statement {
    sid    = "NoChangeToAgentRoles"
    effect = "Deny"
    actions = [
      "iam:AttachRolePolicy",
      "iam:DeleteRole",
      "iam:DeleteRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PassRole",
      "iam:PutRolePermissionsBoundary",
      "iam:PutRolePolicy",
      "iam:TagRole",
      "iam:UntagRole",
      "iam:UpdateAssumeRolePolicy",
      "iam:UpdateRole",
    ]
    resources = ["${local.iam_arn_prefix}:role${local.agent_role_path}*"]
  }

  statement {
    sid    = "NoChangeToStateAndEvidenceBuckets"
    effect = "Deny"
    actions = [
      "s3:DeleteBucket",
      "s3:DeleteBucketPolicy",
      "s3:PutBucketAcl",
      "s3:PutBucketPolicy",
      "s3:PutBucketPublicAccessBlock",
      "s3:PutBucketVersioning",
      "s3:PutEncryptionConfiguration",
      "s3:PutLifecycleConfiguration",
    ]
    resources = local.protected_bucket_arns
  }

  statement {
    sid       = "NoEvidenceDeletion"
    effect    = "Deny"
    actions   = ["s3:DeleteObject", "s3:DeleteObjectVersion"]
    resources = ["${var.evidence_bucket_arn}/*"]
  }

  # The settings of the control plane include its signing key and its database URL. A plan
  # runs code from a pull request, so no agent role may read them.
  statement {
    sid       = "NoControlPlaneSettings"
    effect    = "Deny"
    actions   = ["ssm:GetParameter*"]
    resources = local.core_parameter_arns
  }

  # The job token of a runner is an environment override of ecs:RunTask. DescribeTasks shows
  # the overrides of a task, and ListTasks finds the tasks. The Allow statement above has no
  # ecs action, so this statement keeps it that way if a later change adds one.
  statement {
    sid       = "NoRunnerTaskDetails"
    effect    = "Deny"
    actions   = ["ecs:DescribeTasks", "ecs:ListTasks"]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "boundary" {
  name        = local.boundary_name
  description = "Permission boundary for the Clanker agent roles and each role that they create."
  policy      = data.aws_iam_policy_document.boundary.json
}
