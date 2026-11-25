# An internal Application Load Balancer ends TLS for the core and for Keycloak. Its two
# names are in a public zone and point to private addresses.

resource "aws_acm_certificate" "this" {
  domain_name               = local.core_host
  subject_alternative_names = [local.idp_host]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# The keys are the two names, which are known before the certificate exists.
resource "aws_route53_record" "validation" {
  for_each = toset([local.core_host, local.idp_host])

  zone_id         = var.zone_id
  allow_overwrite = true
  ttl             = 60
  name = one([
    for option in aws_acm_certificate.this.domain_validation_options :
    option.resource_record_name if option.domain_name == each.key
  ])
  type = one([
    for option in aws_acm_certificate.this.domain_validation_options :
    option.resource_record_type if option.domain_name == each.key
  ])
  records = [one([
    for option in aws_acm_certificate.this.domain_validation_options :
    option.resource_record_value if option.domain_name == each.key
  ])]
}

resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [for record in aws_route53_record.validation : record.fqdn]
}

resource "aws_lb" "this" {
  name                       = local.name
  internal                   = true
  load_balancer_type         = "application"
  subnets                    = var.private_subnet_ids
  security_groups            = [aws_security_group.load_balancer.id]
  drop_invalid_header_fields = true
}

# A target group name has 32 characters or fewer, so the provider adds a suffix to a prefix.
resource "aws_lb_target_group" "core" {
  name_prefix          = "core-"
  port                 = local.core_port
  protocol             = "HTTP"
  target_type          = "instance"
  vpc_id               = var.vpc_id
  deregistration_delay = 30

  health_check {
    path                = "/healthz"
    matcher             = "200"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Name = "${local.name}-core" }
}

# Keycloak answers its discovery document only when the realm exists, so a healthy target
# can sign people in.
resource "aws_lb_target_group" "keycloak" {
  name_prefix          = "idp-"
  port                 = local.keycloak_port
  protocol             = "HTTP"
  target_type          = "ip"
  vpc_id               = var.vpc_id
  deregistration_delay = 30

  health_check {
    path                = "/realms/${local.realm_name}/.well-known/openid-configuration"
    matcher             = "200"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Name = "${local.name}-idp" }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.this.certificate_arn

  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "text/plain"
      message_body = "Unknown host"
      status_code  = "404"
    }
  }
}

resource "aws_lb_listener_rule" "core" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 10

  condition {
    host_header {
      values = [local.core_host]
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.core.arn
  }
}

resource "aws_lb_listener_rule" "keycloak" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 20

  condition {
    host_header {
      values = [local.idp_host]
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.keycloak.arn
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_route53_record" "alias" {
  for_each = { core = local.core_host, idp = local.idp_host }

  zone_id = var.zone_id
  name    = each.value
  type    = "A"

  alias {
    name                   = aws_lb.this.dns_name
    zone_id                = aws_lb.this.zone_id
    evaluate_target_health = false
  }
}
