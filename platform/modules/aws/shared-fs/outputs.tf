output "file_system_id" {
  description = "EFS file system ID. It is known only after each mount target exists."
  value       = aws_efs_file_system.this.id

  depends_on = [aws_efs_mount_target.this]
}
