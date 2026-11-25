# Keycloak is the identity provider of the core: one Fargate task behind the load balancer,
# with the realm of the input keycloak_realm and its users in a file database of the task
# (--db=dev-file). S.R.C. does not manage it.

locals {
  keycloak_digest    = "sha256:7e66a96e4739239070aeaf72e4c5decf0f7f682cfc725b9f7ea2b336eaba38b3"
  keycloak_image     = "quay.io/keycloak/keycloak:26.8.2@${local.keycloak_digest}"
  keycloak_container = "keycloak"

  realm       = jsondecode(var.keycloak_realm)
  realm_name  = local.realm.realm
  client_id   = local.realm.clients[0].clientId
  oidc_issuer = "${local.idp_url}/realms/${local.realm_name}"

  # Keycloak replaces ${NAME} in the realm with the variable NAME when it imports the realm,
  # and keeps the text ${NAME} when the variable is not set.
  realm_variables = sort(distinct([
    for match in regexall("\\$\\{([A-Za-z_][A-Za-z0-9_]*)\\}", var.keycloak_realm) : match[0]
  ]))
  realm_values = {
    CLANKER_PUBLIC_URL   = local.core_url
    CLANKER_ADMIN_EMAIL  = local.admin_email
    REALM_APPROVER_EMAIL = local.approver_email
  }
  realm_variables_given = setunion(
    [for name, value in local.realm_values : name if value != ""],
    local.idp_setting_names,
  )
  realm_variables_missing = join(", ", sort(setsubtract(
    local.realm_variables, local.realm_variables_given,
  )))

  keycloak_start = file("${path.module}/keycloak-start.sh")

  keycloak_environment = merge(local.realm_values, {
    KC_DB            = "dev-file"
    KC_HOSTNAME      = local.idp_url
    KC_HTTP_ENABLED  = "true"
    KC_PROXY_HEADERS = "xforwarded"
    REALM_JSON       = var.keycloak_realm
  })
}

resource "aws_cloudwatch_log_group" "keycloak" {
  name              = local.keycloak_log_group
  retention_in_days = 7
}

resource "aws_ecs_task_definition" "keycloak" {
  family                   = "${local.name}-keycloak"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 1024
  memory                   = 2048
  execution_role_arn       = aws_iam_role.keycloak_execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  # keycloak-start.sh stops the container when a variable of the realm is missing, and
  # then starts Keycloak in production mode with the realm.
  container_definitions = jsonencode([{
    name       = local.keycloak_container
    image      = local.keycloak_image
    essential  = true
    entryPoint = ["/bin/bash", "-c"]
    command    = concat([local.keycloak_start, "keycloak-start"], local.realm_variables)
    portMappings = [{
      containerPort = local.keycloak_port
      protocol      = "tcp"
    }]
    environment = [for name, value in local.keycloak_environment : { name = name, value = value }]
    secrets = [
      for name in local.idp_setting_names :
      { name = name, valueFrom = aws_ssm_parameter.idp[name].arn }
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.keycloak.name
        awslogs-region        = var.region
        awslogs-stream-prefix = local.keycloak_container
      }
    }
  }])

  lifecycle {
    precondition {
      condition     = local.realm_variables_missing == ""
      error_message = "Keycloak lacks these realm variables: ${local.realm_variables_missing}."
    }
  }
}

# One task at a time: each task has its own file database. The apply waits until the task
# is healthy behind the load balancer, so the core finds the provider when it starts.
resource "aws_ecs_service" "keycloak" {
  name                               = "${local.name}-keycloak"
  cluster                            = aws_ecs_cluster.this.arn
  task_definition                    = aws_ecs_task_definition.keycloak.arn
  desired_count                      = 1
  launch_type                        = "FARGATE"
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100
  health_check_grace_period_seconds  = 300
  enable_ecs_managed_tags            = true
  propagate_tags                     = "SERVICE"
  wait_for_steady_state              = true

  # A task that stops, for example on a missing variable of the realm, fails the
  # deployment and the apply.
  deployment_circuit_breaker {
    enable   = true
    rollback = false
  }

  network_configuration {
    subnets          = var.private_subnet_ids
    security_groups  = [aws_security_group.keycloak.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.keycloak.arn
    container_name   = local.keycloak_container
    container_port   = local.keycloak_port
  }

  depends_on = [aws_lb_listener_rule.keycloak, aws_iam_role_policy.keycloak_execution]
}
