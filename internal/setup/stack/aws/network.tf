# Security groups. Without an egress rule a group sends nothing: OpenTofu removes the rule
# that AWS adds to a new group.

resource "aws_security_group" "load_balancer" {
  name        = "${local.name}-load-balancer"
  description = "Internal load balancer of the core and of Keycloak"
  vpc_id      = var.vpc_id

  tags = { Name = "${local.name}-load-balancer" }
}

resource "aws_security_group" "core" {
  name        = "${local.name}-core"
  description = "Core of the control plane"
  vpc_id      = var.vpc_id

  tags = { Name = "${local.name}-core" }
}

resource "aws_security_group" "runner" {
  name        = "${local.name}-runner"
  description = "Runner tasks of the control plane"
  vpc_id      = var.vpc_id

  tags = { Name = "${local.name}-runner" }
}

resource "aws_security_group" "keycloak" {
  name        = "${local.name}-keycloak"
  description = "Keycloak, the identity provider of the core"
  vpc_id      = var.vpc_id

  tags = { Name = "${local.name}-keycloak" }
}

resource "aws_security_group" "database" {
  name        = "${local.name}-database"
  description = "PostgreSQL of the core"
  vpc_id      = var.vpc_id

  tags = { Name = "${local.name}-database" }
}

locals {
  # People come through the subnet router. The core and the runners are in the VPC.
  load_balancer_ports = { https = 443, http = 80 }
}

resource "aws_vpc_security_group_ingress_rule" "load_balancer_from_router" {
  for_each = local.load_balancer_ports

  security_group_id            = aws_security_group.load_balancer.id
  description                  = "${upper(each.key)} from the subnet router"
  referenced_security_group_id = var.router_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = each.value
  to_port                      = each.value
}

resource "aws_vpc_security_group_ingress_rule" "load_balancer_from_vpc" {
  for_each = local.load_balancer_ports

  security_group_id = aws_security_group.load_balancer.id
  description       = "${upper(each.key)} from the VPC"
  cidr_ipv4         = data.aws_vpc.this.cidr_block
  ip_protocol       = "tcp"
  from_port         = each.value
  to_port           = each.value
}

resource "aws_vpc_security_group_egress_rule" "load_balancer_to_core" {
  security_group_id            = aws_security_group.load_balancer.id
  description                  = "Requests and health checks to the core"
  referenced_security_group_id = aws_security_group.core.id
  ip_protocol                  = "tcp"
  from_port                    = local.core_port
  to_port                      = local.core_port
}

resource "aws_vpc_security_group_egress_rule" "load_balancer_to_keycloak" {
  security_group_id            = aws_security_group.load_balancer.id
  description                  = "Requests and health checks to Keycloak"
  referenced_security_group_id = aws_security_group.keycloak.id
  ip_protocol                  = "tcp"
  from_port                    = local.keycloak_port
  to_port                      = local.keycloak_port
}

resource "aws_vpc_security_group_ingress_rule" "core_from_load_balancer" {
  security_group_id            = aws_security_group.core.id
  description                  = "The core from the load balancer"
  referenced_security_group_id = aws_security_group.load_balancer.id
  ip_protocol                  = "tcp"
  from_port                    = local.core_port
  to_port                      = local.core_port
}

resource "aws_vpc_security_group_ingress_rule" "core_from_runner" {
  security_group_id            = aws_security_group.core.id
  description                  = "The core from its runners"
  referenced_security_group_id = aws_security_group.runner.id
  ip_protocol                  = "tcp"
  from_port                    = local.core_port
  to_port                      = local.core_port
}

resource "aws_vpc_security_group_ingress_rule" "keycloak_from_load_balancer" {
  security_group_id            = aws_security_group.keycloak.id
  description                  = "Keycloak from the load balancer"
  referenced_security_group_id = aws_security_group.load_balancer.id
  ip_protocol                  = "tcp"
  from_port                    = local.keycloak_port
  to_port                      = local.keycloak_port
}

resource "aws_vpc_security_group_ingress_rule" "database_from_core" {
  security_group_id            = aws_security_group.database.id
  description                  = "PostgreSQL from the core"
  referenced_security_group_id = aws_security_group.core.id
  ip_protocol                  = "tcp"
  from_port                    = local.database_port
  to_port                      = local.database_port
}

# The core, the runners and Keycloak reach AWS APIs, the model and the image registries
# through the NAT gateway of the VPC.
resource "aws_vpc_security_group_egress_rule" "all" {
  for_each = {
    core     = aws_security_group.core.id
    runner   = aws_security_group.runner.id
    keycloak = aws_security_group.keycloak.id
  }

  security_group_id = each.value
  description       = "All traffic out"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
