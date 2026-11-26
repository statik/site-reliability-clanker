output "host" {
  description = "Private DNS name of the instance."
  value       = aws_db_instance.this.address
}

output "port" {
  description = "Port."
  value       = aws_db_instance.this.port
}

output "admin_secret_arn" {
  description = "ARN of the Secrets Manager secret. Its JSON has the keys username and password."
  value       = aws_db_instance.this.master_user_secret[0].secret_arn
}

output "engine_version" {
  description = "PostgreSQL version that runs now."
  value       = aws_db_instance.this.engine_version_actual
}
