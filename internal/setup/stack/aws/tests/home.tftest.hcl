# The home stack with mock providers: no AWS account is used. A mock provider cannot render
# a policy document, so the runs check the statements that a document renders into JSON.

mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "123456789012"
      arn        = "arn:aws:sts::123456789012:assumed-role/operator/session"
    }
  }

  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }

  mock_data "aws_iam_session_context" {
    defaults = { issuer_arn = "arn:aws:iam::123456789012:role/admin/operator" }
  }

  mock_data "aws_vpc" {
    defaults = { cidr_block = "10.20.0.0/16" }
  }

  mock_data "aws_route53_zone" {
    defaults = { name = "example.com" }
  }

  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }

  mock_resource "aws_acm_certificate" {
    defaults = {
      arn = "arn:aws:acm:us-west-2:123456789012:certificate/test"
      domain_validation_options = [
        {
          domain_name           = "src-test.example.com"
          resource_record_name  = "_a.src-test.example.com."
          resource_record_type  = "CNAME"
          resource_record_value = "_a.acm-validations.aws."
        },
        {
          domain_name           = "src-test-idp.example.com"
          resource_record_name  = "_b.src-test-idp.example.com."
          resource_record_type  = "CNAME"
          resource_record_value = "_b.acm-validations.aws."
        },
      ]
    }
  }

  mock_resource "aws_db_instance" {
    defaults = { address = "clanker-test.abcdefghijkl.us-west-2.rds.amazonaws.com" }
  }

  mock_resource "aws_lb" {
    defaults = {
      arn      = "arn:aws:elasticloadbalancing:us-west-2:123456789012:loadbalancer/app/test/1"
      dns_name = "internal-clanker-test-1.us-west-2.elb.amazonaws.com"
      zone_id  = "ZLBTEST"
    }
  }

  mock_resource "aws_lb_listener" {
    defaults = {
      arn = "arn:aws:elasticloadbalancing:us-west-2:123456789012:listener/app/test/1/2"
    }
  }

  mock_resource "aws_lb_target_group" {
    defaults = {
      arn = "arn:aws:elasticloadbalancing:us-west-2:123456789012:targetgroup/test/3"
    }
  }

  mock_resource "aws_iam_instance_profile" {
    defaults = { arn = "arn:aws:iam::123456789012:instance-profile/clanker-test-core" }
  }

  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/test" }
  }

  mock_resource "aws_ssm_parameter" {
    defaults = { arn = "arn:aws:ssm:us-west-2:123456789012:parameter/src/test/x" }
  }

  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-west-2:123456789012:log-group:test" }
  }

  mock_resource "aws_ecs_cluster" {
    defaults = { arn = "arn:aws:ecs:us-west-2:123456789012:cluster/clanker-test" }
  }

  mock_resource "aws_ecs_task_definition" {
    defaults = {
      arn = "arn:aws:ecs:us-west-2:123456789012:task-definition/clanker-test-runner:1"
    }
  }

  mock_resource "aws_ecr_repository" {
    defaults = {
      arn            = "arn:aws:ecr:us-west-2:123456789012:repository/clanker-test-runner"
      repository_url = "123456789012.dkr.ecr.us-west-2.amazonaws.com/clanker-test-runner"
    }
  }

  mock_resource "aws_launch_template" {
    defaults = { id = "lt-0123456789abcdef0" }
  }

  mock_resource "aws_kms_key" {
    defaults = { arn = "arn:aws:kms:us-west-2:123456789012:key/test" }
  }
}

mock_provider "random" {}

mock_provider "tls" {}

# The Ed25519 key of test 1 in RFC 8032, so that the signing key has a known value.
override_resource {
  target = tls_private_key.signing
  values = {
    private_key_pem_pkcs8 = <<-EOT
      -----BEGIN PRIVATE KEY-----
      MC4CAQAwBQYDK2VwBCIEIJ1hsZ3v/VpguoRK9JLsLMREScVpezJpGXA7rAMcrn9g
      -----END PRIVATE KEY-----
    EOT
    public_key_pem        = <<-EOT
      -----BEGIN PUBLIC KEY-----
      MCowBQYDK2VwAyEA11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=
      -----END PUBLIC KEY-----
    EOT
  }
}

override_resource {
  target = aws_iam_role.core
  values = { arn = "arn:aws:iam::123456789012:role/clanker-test-core" }
}

override_resource {
  target = aws_iam_role.runner_execution
  values = { arn = "arn:aws:iam::123456789012:role/clanker-test-runner-execution" }
}

override_resource {
  target = aws_iam_role.keycloak_execution
  values = { arn = "arn:aws:iam::123456789012:role/clanker-test-keycloak-execution" }
}

override_resource {
  target = aws_ecr_repository.application
  values = {
    repository_url = "123456789012.dkr.ecr.us-west-2.amazonaws.com/clanker-test/paperboat"
  }
}

override_resource {
  target = aws_security_group.runner
  values = { id = "sg-0123456789abcdef0" }
}

variables {
  env_name                 = "test"
  region                   = "us-west-2"
  vpc_id                   = "vpc-0123456789abcdef0"
  private_subnet_ids       = ["subnet-0123456789abcdef0", "subnet-0123456789abcdef1"]
  router_security_group_id = "sg-0fedcba9876543210"
  zone_id                  = "ZZONETEST"
  required_tags            = { "example:team" = "platform", "example:owner" = "owner@example.com" }
  keycloak_realm           = file("../keycloak-realm.json")
}

run "phase_one_creates_no_instance_and_no_runner_task" {
  command = apply

  assert {
    condition     = length(aws_launch_template.core) == 0 && length(aws_autoscaling_group.core) == 0
    error_message = "Phase 1 must not create the instance of the core."
  }

  assert {
    condition = (
      length(aws_ecs_task_definition.runner) == 0 &&
      !contains(keys(aws_ssm_parameter.core), "CLANKER_AWS_RUNNER")
    )
    error_message = "Phase 1 must not create the runner task definition or CLANKER_AWS_RUNNER."
  }

  assert {
    condition     = aws_ssm_parameter.bootstrap.name == "/src/test/CLANKER_AWS_BOOTSTRAP"
    error_message = "Phase 1 must create the slot of CLANKER_AWS_BOOTSTRAP."
  }

  assert {
    condition = (
      output.runner_task_definition_arn == null && output.core_autoscaling_group == null
    )
    error_message = "The outputs of phase 2 must be null in phase 1."
  }
}

run "load_balancer_is_internal_with_tls" {
  command = apply

  assert {
    condition     = aws_lb.this.internal && aws_lb.this.load_balancer_type == "application"
    error_message = "The load balancer must be an internal ALB."
  }

  assert {
    condition     = toset(aws_lb.this.subnets) == toset(var.private_subnet_ids)
    error_message = "The load balancer must be in the private subnets."
  }

  assert {
    condition = (
      aws_lb_listener.https.protocol == "HTTPS" && aws_lb_listener.https.port == 443 &&
      aws_lb_listener.https.certificate_arn == aws_acm_certificate_validation.this.certificate_arn
    )
    error_message = "The listener must serve HTTPS on 443 with the validated certificate."
  }

  assert {
    condition = (
      aws_lb_listener.http.port == 80 &&
      aws_lb_listener.http.default_action[0].type == "redirect" &&
      aws_lb_listener.http.default_action[0].redirect[0].protocol == "HTTPS"
    )
    error_message = "Port 80 must redirect to HTTPS."
  }

  assert {
    condition = (
      [for c in aws_lb_listener_rule.core.condition : c.host_header[0].values] ==
      [toset(["src-test.example.com"])] &&
      aws_lb_listener_rule.core.action[0].target_group_arn == aws_lb_target_group.core.arn &&
      aws_lb_target_group.core.port == 7070 &&
      [for c in aws_lb_listener_rule.keycloak.condition : c.host_header[0].values] ==
      [toset(["src-test-idp.example.com"])] &&
      aws_lb_listener_rule.keycloak.action[0].target_group_arn == aws_lb_target_group.keycloak.arn
    )
    error_message = "Each name must go to its own target group, the core on port 7070."
  }

  assert {
    condition = (
      aws_acm_certificate.this.domain_name == "src-test.example.com" &&
      aws_acm_certificate.this.subject_alternative_names == toset(["src-test-idp.example.com"]) &&
      toset(keys(aws_route53_record.validation)) ==
      toset(["src-test.example.com", "src-test-idp.example.com"])
    )
    error_message = "The certificate must cover both names, with a validation record for each."
  }

  assert {
    condition = alltrue([
      for record in aws_route53_record.alias :
      record.alias[0].name == aws_lb.this.dns_name && record.type == "A"
    ])
    error_message = "Both names must be aliases of the load balancer."
  }

  assert {
    condition = (
      toset([
        for rule in aws_vpc_security_group_ingress_rule.load_balancer_from_router :
        rule.referenced_security_group_id
      ]) == toset([var.router_security_group_id]) &&
      toset([
        for rule in aws_vpc_security_group_ingress_rule.load_balancer_from_vpc : rule.cidr_ipv4
      ]) == toset(["10.20.0.0/16"]) &&
      toset([
        for rule in aws_vpc_security_group_ingress_rule.load_balancer_from_router : rule.from_port
      ]) == toset([80, 443])
    )
    error_message = "The load balancer must admit 443 and 80 only from the router and the VPC."
  }
}

run "no_value_holds_an_instance_address" {
  command = apply

  assert {
    condition = (
      output.core_url == "https://src-test.example.com" &&
      output.collector_endpoint == "https://src-test.example.com/otlp" &&
      output.oidc_issuer == "https://src-test-idp.example.com/realms/clanker" &&
      aws_ssm_parameter.core["CLANKER_PUBLIC_URL"].value == "https://src-test.example.com" &&
      aws_ssm_parameter.core["CLANKER_OIDC_ISSUER"].value == output.oidc_issuer
    )
    error_message = "The URLs must use the names on the load balancer."
  }

  assert {
    condition = alltrue([
      for value in concat(
        [output.core_url, output.collector_endpoint, output.idp_url, output.oidc_issuer],
        [for parameter in aws_ssm_parameter.core : nonsensitive(parameter.value)],
        [
          for variable in jsondecode(
            aws_ecs_task_definition.keycloak.container_definitions
          )[0].environment : variable.value
        ],
      ) : length(regexall("(^|[^0-9.])[0-9]{1,3}(\\.[0-9]{1,3}){3}([^0-9.]|$)", value)) == 0
    ])
    error_message = "No output, parameter or variable of Keycloak may hold an IP address."
  }

  assert {
    condition = aws_ssm_parameter.core["CLANKER_DATABASE_URL"].value == join("", [
      "postgres://clanker:", random_password.database.result,
      "@clanker-test.abcdefghijkl.us-west-2.rds.amazonaws.com:5432/clanker?sslmode=require",
    ])
    error_message = "The database URL must name the RDS host and require TLS."
  }
}

run "settings_are_secure_parameters_below_one_path" {
  command = apply

  assert {
    condition = alltrue([
      for parameter in concat(
        values(aws_ssm_parameter.core), values(aws_ssm_parameter.idp),
        [aws_ssm_parameter.bootstrap],
      ) :
      parameter.type == "SecureString" && parameter.key_id == aws_kms_key.settings.arn &&
      startswith(parameter.name, "/src/test/")
    ])
    error_message = "Each setting must be a SecureString with the key of the stack below /src/test."
  }

  assert {
    condition = toset(keys(aws_ssm_parameter.core)) == toset([
      "CLANKER_DATABASE_URL", "CLANKER_SIGNING_KEY", "CLANKER_PUBLIC_URL", "CLANKER_OIDC_ISSUER",
      "CLANKER_OIDC_CLIENT_ID", "CLANKER_OIDC_CLIENT_SECRET", "CLANKER_ADMIN_EMAIL",
      "CLANKER_APPROVER_EMAILS", "CLANKER_AWS_LAYER_VARIABLES",
    ])
    error_message = "Phase 1 must write the database, the key, the sign-in and the layer inputs."
  }

  assert {
    condition = alltrue([
      for parameter in aws_ssm_parameter.idp : startswith(parameter.name, "/src/test/idp/")
    ])
    error_message = "The settings of Keycloak must be one level below the path of the core."
  }

  assert {
    condition = (
      aws_ssm_parameter.core["CLANKER_SIGNING_KEY"].value ==
      "nWGxne/9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2DXWpgBgrEKt9VL/tPJZAc6DuFy89qmIyWvAhpo9wdRGg=="
    )
    error_message = "The signing key must be the seed and then the public key, in base64."
  }

  assert {
    condition = (
      aws_ssm_parameter.core["CLANKER_ADMIN_EMAIL"].value == "admin@src-test-idp.example.com" &&
      aws_ssm_parameter.core["CLANKER_APPROVER_EMAILS"].value ==
      "approver@src-test-idp.example.com" &&
      aws_ssm_parameter.core["CLANKER_OIDC_CLIENT_ID"].value == "clanker" &&
      aws_ssm_parameter.core["CLANKER_OIDC_CLIENT_SECRET"].value ==
      aws_ssm_parameter.idp["CLANKER_OIDC_CLIENT_SECRET"].value
    )
    error_message = "The core and Keycloak must share the client, its secret and the two approvers."
  }

  assert {
    condition = (
      output.parameter_path == "/src/test" &&
      output.core_image_env.CLANKER_PARAMETER_PATH == "/src/test"
    )
    error_message = "The outputs must name the parameter path."
  }
}

run "the_layers_get_the_inputs_of_the_account" {
  command = apply

  assert {
    condition = jsondecode(nonsensitive(
      aws_ssm_parameter.core["CLANKER_AWS_LAYER_VARIABLES"].value
      )) == {
      existing_network_id         = "vpc-0123456789abcdef0"
      existing_private_subnet_ids = ["subnet-0123456789abcdef0", "subnet-0123456789abcdef1"]
      image_registry              = "123456789012.dkr.ecr.us-west-2.amazonaws.com/clanker-test"
      instance_tags = {
        "example:team" = "platform", "example:owner" = "owner@example.com"
      }
      operator_principal_arn = "arn:aws:iam::123456789012:role/admin/operator"
    }
    error_message = "The layer inputs must be the network, the registry, the tags and the operator."
  }

  assert {
    condition = (
      output.image_registry == "123456789012.dkr.ecr.us-west-2.amazonaws.com/clanker-test" &&
      aws_ecr_repository.application.name == "clanker-test/paperboat" &&
      aws_ecr_repository.application.force_delete
    )
    error_message = "Paperboat images go to <image_registry>/paperboat, which a destroy removes."
  }

  assert {
    condition     = !contains(keys(aws_ssm_parameter.core), "OPENROUTER_API_KEY")
    error_message = "Without a model key there must be no parameter OPENROUTER_API_KEY."
  }
}

run "a_model_key_becomes_a_setting_of_the_core" {
  command = apply

  variables {
    model_api_key = "sk-or-v1-test-key-of-the-stack"
  }

  assert {
    condition = (
      nonsensitive(aws_ssm_parameter.core["OPENROUTER_API_KEY"].value) ==
      "sk-or-v1-test-key-of-the-stack" &&
      aws_ssm_parameter.core["OPENROUTER_API_KEY"].name == "/src/test/OPENROUTER_API_KEY"
    )
    error_message = "The model key must be the parameter OPENROUTER_API_KEY below /src/test."
  }
}

run "only_the_core_and_keycloak_decrypt_the_settings" {
  command = apply

  assert {
    condition = toset(flatten([
      for statement in data.aws_iam_policy_document.settings_key.statement : [
        for principal in statement.principals : principal.identifiers
      ] if contains(statement.actions, "kms:Decrypt")
      ])) == toset([
      "arn:aws:iam::123456789012:role/admin/operator",
      "arn:aws:iam::123456789012:role/clanker-test-core",
      "arn:aws:iam::123456789012:role/clanker-test-keycloak-execution",
    ])
    error_message = "Only the operator, the core and Keycloak may decrypt with the key."
  }

  assert {
    condition = alltrue([
      for statement in data.aws_iam_policy_document.settings_key.statement :
      length(setintersection(toset(statement.actions), toset([
        "kms:*", "kms:CreateGrant", "kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey",
        "kms:ReEncryptFrom", "kms:ReEncryptTo",
      ]))) == 0
      if anytrue([
        for principal in statement.principals :
        contains(principal.identifiers, "arn:aws:iam::123456789012:root")
      ])
    ])
    error_message = "The account may manage the key but not use it."
  }

  assert {
    condition = alltrue([
      for statement in data.aws_iam_policy_document.settings_key.statement :
      anytrue([
        for condition in statement.condition :
        condition.variable == "kms:ViaService" &&
        toset(condition.values) == toset(["ssm.us-west-2.amazonaws.com"])
      ])
      if contains(statement.actions, "kms:Decrypt")
    ])
    error_message = "The key may decrypt only through Parameter Store."
  }

  assert {
    condition = anytrue([
      for statement in data.aws_iam_policy_document.settings_key.statement :
      statement.sid == "KeycloakReadsItsOwnSettings" && anytrue([
        for condition in statement.condition :
        condition.variable == "kms:EncryptionContext:PARAMETER_ARN" &&
        toset(condition.values) ==
        toset(["arn:aws:ssm:us-west-2:123456789012:parameter/src/test/idp/*"])
      ])
    ])
    error_message = "Keycloak may decrypt only its own settings."
  }

  assert {
    condition = (
      aws_kms_key.settings.deletion_window_in_days == 7 &&
      aws_kms_key.settings.policy == data.aws_iam_policy_document.settings_key.json
    )
    error_message = "The key must have its policy and a deletion window of 7 days."
  }
}

run "the_core_role_has_only_its_statements" {
  command = apply

  assert {
    condition = {
      for statement in data.aws_iam_policy_document.core.statement :
      statement.sid => {
        actions   = toset(statement.actions)
        resources = toset(statement.resources)
      }
      } == {
      ReadTheSettings = {
        actions = toset(["ssm:GetParametersByPath"])
        resources = toset([
          "arn:aws:ssm:us-west-2:123456789012:parameter/src/test",
          "arn:aws:ssm:us-west-2:123456789012:parameter/src/test/*",
        ])
      }
      DecryptTheSettings = {
        actions   = toset(["kms:Decrypt"])
        resources = toset(["arn:aws:kms:us-west-2:123456789012:key/test"])
      }
      AssumeTheAgentRoles = {
        actions   = toset(["sts:AssumeRole", "sts:TagSession"])
        resources = toset(["arn:aws:iam::123456789012:role/clanker/clanker-test-*"])
      }
      StartRunners = {
        actions = toset(["ecs:RunTask"])
        resources = toset([
          "arn:aws:ecs:us-west-2:123456789012:task-definition/clanker-test-runner:*",
        ])
      }
      WatchAndStopRunners = {
        actions   = toset(["ecs:DescribeTasks", "ecs:StopTask", "ecs:TagResource"])
        resources = toset(["arn:aws:ecs:us-west-2:123456789012:task/clanker-test/*"])
      }
      PassTheRunnerExecutionRole = {
        actions   = toset(["iam:PassRole"])
        resources = toset(["arn:aws:iam::123456789012:role/clanker-test-runner-execution"])
      }
    }
    error_message = "The role of the core must have exactly its six statements."
  }

  assert {
    condition = anytrue([
      for statement in data.aws_iam_policy_document.core.statement :
      statement.sid == "StartRunners" && anytrue([
        for condition in statement.condition :
        condition.test == "ArnEquals" && condition.variable == "ecs:cluster" &&
        toset(condition.values) ==
        toset(["arn:aws:ecs:us-west-2:123456789012:cluster/clanker-test"])
      ])
    ])
    error_message = "The core may start runners only in its cluster."
  }

  assert {
    condition = anytrue([
      for statement in data.aws_iam_policy_document.core.statement :
      statement.sid == "PassTheRunnerExecutionRole" && anytrue([
        for condition in statement.condition :
        condition.variable == "iam:PassedToService" &&
        toset(condition.values) == toset(["ecs-tasks.amazonaws.com"])
      ])
    ])
    error_message = "The core may pass the execution role only to ECS tasks."
  }

  assert {
    condition = (
      aws_iam_role_policy.core.policy == data.aws_iam_policy_document.core.json &&
      output.core_role_arn == "arn:aws:iam::123456789012:role/clanker-test-core"
    )
    error_message = "The role of the core must carry the document; its ARN is the broker principal."
  }
}

run "keycloak_gets_every_variable_of_the_realm" {
  command = apply

  assert {
    condition = jsondecode(aws_ecs_task_definition.keycloak.container_definitions)[0].command == [
      file("keycloak-start.sh"), "keycloak-start", "CLANKER_ADMIN_EMAIL",
      "CLANKER_OIDC_CLIENT_SECRET", "CLANKER_PUBLIC_URL", "REALM_ADMIN_PASSWORD",
      "REALM_APPROVER_EMAIL", "REALM_APPROVER_PASSWORD",
    ]
    error_message = "The entry point must check each variable that the realm reads."
  }

  assert {
    condition = toset([
      for secret in jsondecode(aws_ecs_task_definition.keycloak.container_definitions)[0].secrets :
      secret.name
    ]) == toset(["CLANKER_OIDC_CLIENT_SECRET", "REALM_ADMIN_PASSWORD", "REALM_APPROVER_PASSWORD"])
    error_message = "The secrets of the realm must come from Parameter Store."
  }

  assert {
    condition = {
      for variable in jsondecode(
        aws_ecs_task_definition.keycloak.container_definitions
      )[0].environment :
      variable.name => variable.value if variable.name != "REALM_JSON"
      } == {
      CLANKER_ADMIN_EMAIL  = "admin@src-test-idp.example.com"
      CLANKER_PUBLIC_URL   = "https://src-test.example.com"
      KC_DB                = "dev-file"
      KC_HOSTNAME          = "https://src-test-idp.example.com"
      KC_HTTP_ENABLED      = "true"
      KC_PROXY_HEADERS     = "xforwarded"
      REALM_APPROVER_EMAIL = "approver@src-test-idp.example.com"
    }
    error_message = "Keycloak must run with its name, behind the proxy, with example addresses."
  }

  assert {
    condition = (
      aws_ecs_task_definition.keycloak.task_role_arn == null &&
      aws_ecs_service.keycloak.wait_for_steady_state &&
      aws_ecs_service.keycloak.deployment_circuit_breaker[0].enable &&
      aws_lb_target_group.keycloak.health_check[0].path ==
      "/realms/clanker/.well-known/openid-configuration"
    )
    error_message = "Keycloak must have no task role; the apply must wait until its realm answers."
  }
}

run "the_database_is_private_and_everything_can_be_removed" {
  command = apply

  assert {
    condition = (
      aws_db_instance.core.identifier == "clanker-test-core" &&
      aws_db_instance.core.engine == "postgres" && aws_db_instance.core.engine_version == "18" &&
      aws_db_instance.core.instance_class == "db.t4g.micro" &&
      aws_db_instance.core.storage_encrypted && !aws_db_instance.core.publicly_accessible &&
      !aws_db_instance.core.multi_az &&
      aws_db_instance.core.vpc_security_group_ids == toset([aws_security_group.database.id])
    )
    error_message = "The database must be a private, encrypted PostgreSQL 18 on db.t4g.micro."
  }

  assert {
    condition = (
      !aws_db_instance.core.deletion_protection && aws_db_instance.core.skip_final_snapshot &&
      aws_db_instance.core.delete_automated_backups &&
      aws_ecr_repository.runner.force_delete &&
      aws_ecr_repository.runner.name == "clanker-test/runner" &&
      aws_kms_key.settings.deletion_window_in_days == 7 &&
      alltrue([for record in aws_route53_record.validation : record.allow_overwrite]) &&
      aws_cloudwatch_log_group.runner.retention_in_days == 7 &&
      aws_cloudwatch_log_group.keycloak.retention_in_days == 7
    )
    error_message = "A destroy must remove the database, the repository and the records."
  }
}

run "a_missing_realm_variable_fails_the_plan" {
  command = plan

  variables {
    keycloak_realm = jsonencode({
      realm   = "clanker"
      clients = [{ clientId = "clanker", secret = "$${NOT_GIVEN}" }]
    })
  }

  expect_failures = [aws_ecs_task_definition.keycloak]
}

run "phase_two_starts_the_core_and_the_runners" {
  command = apply

  variables {
    runner_image_digest = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
    core_image_id       = "ami-0123456789abcdef0"
  }

  assert {
    condition = alltrue(flatten([
      for spec in aws_launch_template.core[0].tag_specifications : [
        for key, value in merge(var.required_tags, { clanker_env = "test" }) :
        spec.tags[key] == value
      ]
      ])) && toset([
      for spec in aws_launch_template.core[0].tag_specifications : spec.resource_type
    ]) == toset(["instance", "volume"])
    error_message = "The instance and the volume of the core must carry the required tags."
  }

  assert {
    condition = (
      aws_launch_template.core[0].metadata_options[0].http_tokens == "required" &&
      aws_launch_template.core[0].key_name == null &&
      aws_launch_template.core[0].image_id == "ami-0123456789abcdef0" &&
      aws_launch_template.core[0].instance_type == "t4g.small"
    )
    error_message = "The core must need IMDSv2 and have no key pair."
  }

  assert {
    condition = (
      aws_autoscaling_group.core[0].min_size == 1 && aws_autoscaling_group.core[0].max_size == 1 &&
      aws_autoscaling_group.core[0].desired_capacity == 1 &&
      aws_autoscaling_group.core[0].health_check_type == "ELB" &&
      aws_autoscaling_group.core[0].target_group_arns == toset([aws_lb_target_group.core.arn])
    )
    error_message = "The core must be a group of one with the health check of the load balancer."
  }

  assert {
    condition = (
      aws_ecs_task_definition.runner[0].task_role_arn == null &&
      aws_ecs_task_definition.runner[0].execution_role_arn ==
      "arn:aws:iam::123456789012:role/clanker-test-runner-execution" &&
      aws_ecs_task_definition.runner[0].runtime_platform[0].cpu_architecture == "ARM64" &&
      jsondecode(aws_ecs_task_definition.runner[0].container_definitions)[0].image == join("", [
        "123456789012.dkr.ecr.us-west-2.amazonaws.com/clanker-test-runner@",
        "sha256:0000000000000000000000000000000000000000000000000000000000000001",
      ])
    )
    error_message = "The runner must have no task role and run the image by its digest on ARM64."
  }

  assert {
    condition = jsondecode(nonsensitive(aws_ssm_parameter.core["CLANKER_AWS_RUNNER"].value)) == {
      cluster         = "arn:aws:ecs:us-west-2:123456789012:cluster/clanker-test"
      task_definition = "arn:aws:ecs:us-west-2:123456789012:task-definition/clanker-test-runner:1"
      container       = "runner"
      subnets         = ["subnet-0123456789abcdef0", "subnet-0123456789abcdef1"]
      security_groups = ["sg-0123456789abcdef0"]
    }
    error_message = "CLANKER_AWS_RUNNER must have exactly the five fields of awsrunner.ParseConfig."
  }
}

run "the_core_image_needs_the_runner_image" {
  command = plan

  variables {
    core_image_id = "ami-0123456789abcdef0"
  }

  expect_failures = [aws_launch_template.core]
}
