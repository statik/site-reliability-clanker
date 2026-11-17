data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

locals {
  account_id  = data.aws_caller_identity.current.account_id
  partition   = data.aws_partition.current.partition
  name_prefix = "clanker-${var.env_name}"

  agent_role_path    = "/clanker/"
  workload_role_path = "/clanker-workload/"
  iam_arn_prefix     = "arn:${local.partition}:iam::${local.account_id}"

  boundary_name = "${local.name_prefix}-boundary"
  boundary_arn  = "${local.iam_arn_prefix}:policy/${local.boundary_name}"

  standing_roles = toset(["observer", "planner"])

  applier_roles = { for layer in var.applier_layers : "applier-${layer}" => layer }

  # Roles with no standing use. The value is the layer that the session must name, or "" when
  # the role is not tied to a layer.
  gated_roles = merge(local.applier_roles, { debugger = "" })
}

data "aws_iam_policy_document" "trust_standing" {
  statement {
    sid     = "BrokerOnly"
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]

    principals {
      type        = "AWS"
      identifiers = [var.broker_principal]
    }
  }
}

data "aws_iam_policy_document" "trust_gated" {
  for_each = local.gated_roles

  statement {
    sid     = "BrokerOnlyWithGrant"
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]

    principals {
      type        = "AWS"
      identifiers = [var.broker_principal]
    }

    condition {
      test     = "StringLike"
      variable = "aws:RequestTag/clanker_grant"
      values   = ["?*"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/clanker_env"
      values   = [var.env_name]
    }

    dynamic "condition" {
      for_each = each.value == "" ? [] : [each.value]

      content {
        test     = "StringEquals"
        variable = "aws:RequestTag/clanker_layer"
        values   = [condition.value]
      }
    }
  }
}

resource "aws_iam_role" "standing" {
  for_each = local.standing_roles

  name                 = "${local.name_prefix}-${each.key}"
  path                 = local.agent_role_path
  assume_role_policy   = data.aws_iam_policy_document.trust_standing.json
  permissions_boundary = aws_iam_policy.boundary.arn
  max_session_duration = 3600
}

resource "aws_iam_role" "gated" {
  for_each = local.gated_roles

  name                 = "${local.name_prefix}-${each.key}"
  path                 = local.agent_role_path
  assume_role_policy   = data.aws_iam_policy_document.trust_gated[each.key].json
  permissions_boundary = aws_iam_policy.boundary.arn
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "observer" {
  for_each = toset(["job-function/ViewOnlyAccess", "CloudWatchReadOnlyAccess"])

  role       = aws_iam_role.standing["observer"].name
  policy_arn = "arn:${local.partition}:iam::aws:policy/${each.key}"
}

resource "aws_iam_role_policy" "observer" {
  name   = "inventory"
  role   = aws_iam_role.standing["observer"].id
  policy = data.aws_iam_policy_document.observer.json
}

resource "aws_iam_role_policy_attachment" "planner" {
  role       = aws_iam_role.standing["planner"].name
  policy_arn = "arn:${local.partition}:iam::aws:policy/ReadOnlyAccess"
}

resource "aws_iam_role_policy" "planner" {
  name   = "plan"
  role   = aws_iam_role.standing["planner"].id
  policy = data.aws_iam_policy_document.planner.json
}

resource "aws_iam_role_policy" "debugger" {
  name   = "cluster-login"
  role   = aws_iam_role.gated["debugger"].id
  policy = data.aws_iam_policy_document.debugger.json
}

resource "aws_iam_role_policy" "applier" {
  for_each = local.applier_roles

  name   = "apply-${each.value}"
  role   = aws_iam_role.gated[each.key].id
  policy = data.aws_iam_policy_document.applier[each.value].json
}
