# The database of the core. It holds all state of the control plane, so the instance of the
# core keeps nothing on its disk.

locals {
  database_port = 5432
  database_name = "clanker"
  database_user = "clanker"
}

# Letters and digits only: the password goes into a URL.
resource "random_password" "database" {
  length  = 40
  special = false
}

# 30-data names its database clanker-<env>, so this one has the suffix -core.
resource "aws_db_subnet_group" "core" {
  name       = "${local.name}-core"
  subnet_ids = var.private_subnet_ids
}

resource "aws_db_instance" "core" {
  identifier = "${local.name}-core"

  engine = "postgres"
  # The major version only: RDS picks its current minor version of PostgreSQL 18.
  engine_version             = "18"
  auto_minor_version_upgrade = true
  instance_class             = "db.t4g.micro"

  storage_type      = "gp3"
  allocated_storage = 20
  storage_encrypted = true

  db_name  = local.database_name
  port     = local.database_port
  username = local.database_user
  password = random_password.database.result

  db_subnet_group_name   = aws_db_subnet_group.core.name
  vpc_security_group_ids = [aws_security_group.database.id]
  publicly_accessible    = false
  multi_az               = false

  backup_retention_period  = 1
  delete_automated_backups = true
  deletion_protection      = false
  skip_final_snapshot      = true
  apply_immediately        = true
}

locals {
  # RDS for PostgreSQL requires TLS by default (rds.force_ssl).
  database_url = format("postgres://%s:%s@%s:%d/%s?sslmode=require", local.database_user,
    random_password.database.result, aws_db_instance.core.address, local.database_port,
  local.database_name)
}
