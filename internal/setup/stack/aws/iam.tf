# The role of the core is the broker principal of 00-bootstrap: only it can assume the agent
# roles. The runner has an execution role and no task role, so a job has no AWS credential
# unless the broker gives it one.

locals {
  # 00-bootstrap names the agent roles clanker-<env>-<identity> on this path.
  agent_role_arns = "arn:${local.partition}:iam::${local.account_id}:role/clanker/${local.name}-*"

  ecs_arn             = "arn:${local.partition}:ecs:${local.arn_suffix}"
  cluster_arn         = "${local.ecs_arn}:cluster/${local.name}"
  runner_family       = "${local.name}-runner"
  runner_family_arn   = "${local.ecs_arn}:task-definition/${local.runner_family}:*"
  runner_task_arns    = "${local.ecs_arn}:task/${local.name}/*"
  runner_log_group    = "/clanker/${var.env_name}/runner"
  keycloak_log_group  = "/clanker/${var.env_name}/keycloak"
  log_group_arn       = "arn:${local.partition}:logs:${local.arn_suffix}:log-group"
  runner_logs_arn     = "${local.log_group_arn}:${local.runner_log_group}:*"
  keycloak_logs_arn   = "${local.log_group_arn}:${local.keycloak_log_group}:*"
  ecs_tasks_principal = "ecs-tasks.amazonaws.com"
}

data "aws_iam_policy_document" "ec2_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "ecs_tasks_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = [local.ecs_tasks_principal]
    }
  }
}

resource "aws_iam_role" "core" {
  name               = "${local.name}-core"
  description        = "The core of S.R.C. ${var.env_name}: the broker of the agent roles"
  assume_role_policy = data.aws_iam_policy_document.ec2_trust.json
}

resource "aws_iam_instance_profile" "core" {
  name = "${local.name}-core"
  role = aws_iam_role.core.name
}

data "aws_iam_policy_document" "core" {
  # GetParametersByPath names the path. The ARNs below it cover each parameter.
  statement {
    sid     = "ReadTheSettings"
    effect  = "Allow"
    actions = ["ssm:GetParametersByPath"]
    resources = [
      "${local.parameter_arn}${local.parameter_path}",
      "${local.parameter_arn}${local.parameter_path}/*",
    ]
  }

  statement {
    sid       = "DecryptTheSettings"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.settings.arn]
  }

  statement {
    sid       = "AssumeTheAgentRoles"
    effect    = "Allow"
    actions   = ["sts:AssumeRole", "sts:TagSession"]
    resources = [local.agent_role_arns]
  }

  statement {
    sid       = "StartRunners"
    effect    = "Allow"
    actions   = ["ecs:RunTask"]
    resources = [local.runner_family_arn]

    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [local.cluster_arn]
    }
  }

  # RunTask with tags needs ecs:TagResource on the new task.
  statement {
    sid       = "WatchAndStopRunners"
    effect    = "Allow"
    actions   = ["ecs:DescribeTasks", "ecs:StopTask", "ecs:TagResource"]
    resources = [local.runner_task_arns]
  }

  statement {
    sid       = "PassTheRunnerExecutionRole"
    effect    = "Allow"
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.runner_execution.arn]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = [local.ecs_tasks_principal]
    }
  }
}

resource "aws_iam_role_policy" "core" {
  name   = "core"
  role   = aws_iam_role.core.id
  policy = data.aws_iam_policy_document.core.json
}

resource "aws_iam_role" "runner_execution" {
  name               = "${local.name}-runner-execution"
  description        = "ECS pulls the runner image of S.R.C. ${var.env_name} and sends its logs"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_trust.json
}

data "aws_iam_policy_document" "runner_execution" {
  statement {
    sid       = "SignInToTheRegistry"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid    = "PullTheRunnerImage"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = [aws_ecr_repository.runner.arn]
  }

  statement {
    sid       = "WriteTheRunnerLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = [local.runner_logs_arn]
  }
}

resource "aws_iam_role_policy" "runner_execution" {
  name   = "runner-execution"
  role   = aws_iam_role.runner_execution.id
  policy = data.aws_iam_policy_document.runner_execution.json
}

resource "aws_iam_role" "keycloak_execution" {
  name               = "${local.name}-keycloak-execution"
  description        = "ECS gives Keycloak of S.R.C. ${var.env_name} its secrets and sends its logs"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_trust.json
}

data "aws_iam_policy_document" "keycloak_execution" {
  statement {
    sid       = "ReadTheSettingsOfKeycloak"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = ["${local.parameter_arn}${local.idp_parameter_path}/*"]
  }

  statement {
    sid       = "DecryptTheSettingsOfKeycloak"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.settings.arn]
  }

  statement {
    sid       = "WriteTheKeycloakLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = [local.keycloak_logs_arn]
  }
}

resource "aws_iam_role_policy" "keycloak_execution" {
  name   = "keycloak-execution"
  role   = aws_iam_role.keycloak_execution.id
  policy = data.aws_iam_policy_document.keycloak_execution.json
}
