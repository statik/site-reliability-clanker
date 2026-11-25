# The core: a Nanos unikernel on one instance, in an auto scaling group of one that replaces
# an instance that fails its health check on the load balancer. Phase 2: the image exists
# only after `ops image create`.

locals {
  # Tags on the request of ec2:RunInstances. The provider does not add default_tags there.
  instance_tags = merge(local.tags, { Name = "${local.name}-core" })
}

resource "aws_launch_template" "core" {
  count = local.phase_two ? 1 : 0

  name                   = "${local.name}-core"
  image_id               = var.core_image_id
  instance_type          = var.core_instance_type
  vpc_security_group_ids = [aws_security_group.core.id]
  update_default_version = true

  iam_instance_profile {
    arn = aws_iam_instance_profile.core.arn
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  tag_specifications {
    resource_type = "instance"
    tags          = local.instance_tags
  }

  tag_specifications {
    resource_type = "volume"
    tags          = local.instance_tags
  }

  lifecycle {
    precondition {
      condition     = var.runner_image_digest != null
      error_message = "core_image_id needs runner_image_digest: the core reads CLANKER_AWS_RUNNER."
    }
  }
}

resource "aws_autoscaling_group" "core" {
  count = local.phase_two ? 1 : 0

  name                      = "${local.name}-core"
  min_size                  = 1
  max_size                  = 1
  desired_capacity          = 1
  vpc_zone_identifier       = var.private_subnet_ids
  target_group_arns         = [aws_lb_target_group.core.arn]
  health_check_type         = "ELB"
  health_check_grace_period = 300
  # The apply ends when the core answers /healthz behind the load balancer.
  wait_for_elb_capacity     = 1
  wait_for_capacity_timeout = "15m"

  launch_template {
    id      = aws_launch_template.core[0].id
    version = aws_launch_template.core[0].latest_version
  }

  # A new image or instance type replaces the instance. There is one instance, so the core
  # stops for the time of the replacement.
  instance_refresh {
    strategy = "Rolling"

    preferences {
      min_healthy_percentage = 0
    }
  }

  dynamic "tag" {
    for_each = local.tags

    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = false
    }
  }

  # The core reads every setting and the discovery document of Keycloak when it starts, and
  # stops when one is missing.
  depends_on = [
    aws_ecs_service.keycloak,
    aws_route53_record.alias,
    aws_lb_listener_rule.core,
    aws_ssm_parameter.core,
    aws_ssm_parameter.bootstrap,
    aws_iam_role_policy.core,
    aws_vpc_security_group_ingress_rule.database_from_core,
    aws_vpc_security_group_ingress_rule.core_from_load_balancer,
    aws_vpc_security_group_egress_rule.load_balancer_to_core,
    aws_vpc_security_group_egress_rule.all,
  ]
}
