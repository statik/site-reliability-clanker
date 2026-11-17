locals {
  port = 5432
}

resource "aws_db_subnet_group" "this" {
  name       = var.name
  subnet_ids = var.subnet_ids
}

resource "aws_security_group" "this" {
  name        = "${var.name}-postgres"
  description = "PostgreSQL for ${var.name}"
  vpc_id      = var.vpc_id

  tags = { Name = "${var.name}-postgres" }
}

resource "aws_vpc_security_group_ingress_rule" "clients" {
  security_group_id            = aws_security_group.this.id
  description                  = "PostgreSQL from the cluster nodes"
  referenced_security_group_id = var.client_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = local.port
  to_port                      = local.port
}

resource "aws_db_instance" "this" {
  identifier = var.name

  engine         = "postgres"
  engine_version = var.engine_version
  instance_class = "db.t4g.micro"

  storage_type      = "gp3"
  allocated_storage = var.storage_gb
  storage_encrypted = true

  db_name                     = var.database_name
  port                        = local.port
  username                    = var.admin_username
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.this.id]
  publicly_accessible    = false
  multi_az               = false

  backup_retention_period  = var.backup_retention_days
  delete_automated_backups = false
  copy_tags_to_snapshot    = true
  skip_final_snapshot      = true

  # The agent owns the version. A change of `engine_version` is the only upgrade path.
  auto_minor_version_upgrade  = false
  allow_major_version_upgrade = true
  apply_immediately           = true
}
