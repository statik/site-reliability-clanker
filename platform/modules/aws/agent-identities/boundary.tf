locals {
  protected_bucket_arns = [var.state_bucket_arn, var.evidence_bucket_arn]
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
}

resource "aws_iam_policy" "boundary" {
  name        = local.boundary_name
  description = "Permission boundary for the Clanker agent roles and each role that they create."
  policy      = data.aws_iam_policy_document.boundary.json
}
