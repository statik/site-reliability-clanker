# The permission boundary of the agent roles. A mock provider cannot render a policy
# document, so these runs check the statements that the document renders into JSON.

mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "000000000000" }
  }

  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }

  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }

  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::000000000000:policy/test-boundary" }
  }

  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::000000000000:role/clanker/test-role" }
  }

  mock_resource "aws_s3_bucket" {
    defaults = { arn = "arn:aws:s3:::test-bucket" }
  }
}

variables {
  env_name            = "test-env"
  broker_principal    = "arn:aws:iam::000000000000:role/test-broker"
  applier_layers      = ["10-network", "20-cluster", "30-data", "40-platform", "50-app"]
  state_bucket_arn    = "arn:aws:s3:::test-state"
  evidence_bucket_arn = "arn:aws:s3:::test-evidence"
}

run "boundary_denies_the_settings_of_the_control_plane" {
  command = plan

  module {
    source = "../../../modules/aws/agent-identities"
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.boundary.statement :
      s.sid == "NoControlPlaneSettings" && s.effect == "Deny" &&
      toset(s.actions) == toset(["ssm:GetParameter*"]) &&
      toset(s.resources) == toset([
        "arn:aws:ssm:*:000000000000:parameter/src",
        "arn:aws:ssm:*:000000000000:parameter/src/*",
      ])
    ])
    error_message = "The boundary must deny ssm:GetParameter* on /src and on every parameter below it."
  }

  assert {
    condition     = aws_iam_policy.boundary.policy == data.aws_iam_policy_document.boundary.json
    error_message = "The boundary policy must be the rendered boundary document."
  }
}

run "boundary_denies_the_overrides_of_runner_tasks" {
  command = plan

  module {
    source = "../../../modules/aws/agent-identities"
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.boundary.statement :
      s.sid == "NoRunnerTaskDetails" && s.effect == "Deny" &&
      toset(s.actions) == toset(["ecs:DescribeTasks", "ecs:ListTasks"]) &&
      toset(s.resources) == toset(["*"])
    ])
    error_message = "The boundary must deny ecs:DescribeTasks and ecs:ListTasks on every task."
  }

  assert {
    condition = alltrue(flatten([
      for s in data.aws_iam_policy_document.boundary.statement : [
        for action in s.actions : !startswith(action, "ecs:") if s.effect == "Allow"
      ]
    ]))
    error_message = "The Allow statement of the boundary must not allow an ecs action."
  }
}

run "boundary_follows_a_custom_parameter_path" {
  command = plan

  module {
    source = "../../../modules/aws/agent-identities"
  }

  variables {
    core_parameter_path = "/team/control-plane"
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.boundary.statement :
      s.sid == "NoControlPlaneSettings" && toset(s.resources) == toset([
        "arn:aws:ssm:*:000000000000:parameter/team/control-plane",
        "arn:aws:ssm:*:000000000000:parameter/team/control-plane/*",
      ])
    ])
    error_message = "The deny must name the path in core_parameter_path."
  }
}

run "parameter_path_with_a_trailing_slash_is_refused" {
  command = plan

  module {
    source = "../../../modules/aws/agent-identities"
  }

  variables {
    core_parameter_path = "/src/"
  }

  expect_failures = [var.core_parameter_path]
}

run "parameter_path_without_a_leading_slash_is_refused" {
  command = plan

  module {
    source = "../../../modules/aws/agent-identities"
  }

  variables {
    core_parameter_path = "src"
  }

  expect_failures = [var.core_parameter_path]
}

run "bootstrap_plans_with_a_custom_parameter_path" {
  command = plan

  variables {
    state_location      = "test-state"
    region              = "us-east-1"
    ttl_hours           = 0
    core_parameter_path = "/team/control-plane"
  }
}
