locals {
  nfs_port = 2049
}

resource "aws_efs_file_system" "this" {
  creation_token  = var.name
  encrypted       = true
  throughput_mode = "elastic"

  tags = { Name = var.name }
}

resource "aws_security_group" "this" {
  name        = "${var.name}-efs"
  description = "EFS mount targets for ${var.name}"
  vpc_id      = var.vpc_id

  tags = { Name = "${var.name}-efs" }
}

resource "aws_vpc_security_group_ingress_rule" "clients" {
  security_group_id            = aws_security_group.this.id
  description                  = "NFS from the cluster nodes"
  referenced_security_group_id = var.client_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = local.nfs_port
  to_port                      = local.nfs_port
}

resource "aws_efs_mount_target" "this" {
  count = length(var.subnet_ids)

  file_system_id  = aws_efs_file_system.this.id
  subnet_id       = var.subnet_ids[count.index]
  security_groups = [aws_security_group.this.id]
}
