# The runners: one Fargate task for each job, from the image of build/runner/Dockerfile.

resource "aws_ecs_cluster" "this" {
  name = local.name
}

resource "aws_cloudwatch_log_group" "runner" {
  name              = local.runner_log_group
  retention_in_days = 7
}

locals {
  runner_container = "runner"
}

# Phase 2: the digest exists only after the image is pushed to the repository.
resource "aws_ecs_task_definition" "runner" {
  count = var.runner_image_digest == null ? 0 : 1

  family                   = local.runner_family
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 1024
  memory                   = 2048
  execution_role_arn       = aws_iam_role.runner_execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  # The core gives each task the address of the core and the token of the job.
  container_definitions = jsonencode([{
    name      = local.runner_container
    image     = "${aws_ecr_repository.runner.repository_url}@${var.runner_image_digest}"
    essential = true
    # A stop of the task sends SIGTERM, and SIGKILL after this many seconds (at most 120 on
    # Fargate). The runner interrupts OpenTofu and waits for it to save its state and
    # release its lock. It is runner.TaskStopTime.
    stopTimeout = 120
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.runner.name
        awslogs-region        = var.region
        awslogs-stream-prefix = local.runner_container
      }
    }
  }])
}

locals {
  # CLANKER_AWS_RUNNER, as internal/awsrunner.ParseConfig reads it.
  runner_setting = var.runner_image_digest == null ? null : jsonencode({
    cluster         = aws_ecs_cluster.this.arn
    task_definition = aws_ecs_task_definition.runner[0].arn
    container       = local.runner_container
    subnets         = var.private_subnet_ids
    security_groups = [aws_security_group.runner.id]
  })
}
