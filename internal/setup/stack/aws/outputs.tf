output "core_url" {
  description = "URL of the dashboard and the API: CLANKER_PUBLIC_URL."
  value       = local.core_url
}

output "collector_endpoint" {
  description = "Address of the core for the data of the collector of 40-platform."
  value       = local.collector_endpoint
}

output "idp_url" {
  description = "URL of Keycloak. The stack creates no user of its master realm."
  value       = local.idp_url
}

output "oidc_issuer" {
  description = "Issuer of the realm: CLANKER_OIDC_ISSUER."
  value       = local.oidc_issuer
}

output "admin_email" {
  description = "Email address of the first admin of the core and of its user in Keycloak."
  value       = local.admin_email
}

output "approver_email" {
  description = "Email address of the second user in Keycloak, the second approver of T3."
  value       = local.approver_email
}

output "admin_password" {
  description = "Password of the admin in Keycloak."
  value       = random_password.user["admin"].result
  sensitive   = true
}

output "approver_password" {
  description = "Password of the second user in Keycloak."
  value       = random_password.user["approver"].result
  sensitive   = true
}

output "parameter_path" {
  description = "Parameter Store path of the settings of the core: CLANKER_PARAMETER_PATH."
  value       = local.parameter_path
}

output "parameter_key_arn" {
  description = "KMS key of the settings. A writer of bootstrap_parameter passes it as --key-id."
  value       = aws_kms_key.settings.arn
}

output "bootstrap_parameter" {
  description = "Slot for the outputs of 00-bootstrap. Overwrite it with parameter_key_arn."
  value       = aws_ssm_parameter.bootstrap.name
}

output "core_image_env" {
  description = "The Env of the Nanos image of the core. The image holds no other setting."
  value = {
    CLANKER_PARAMETER_PATH = local.parameter_path
    AWS_REGION             = var.region
  }
}

output "core_role_arn" {
  description = "Role of the core: broker_principal of 00-bootstrap."
  value       = aws_iam_role.core.arn
}

output "core_security_group_id" {
  description = "Security group of the core instance."
  value       = aws_security_group.core.id
}

output "runner_repository_url" {
  description = "ECR repository of the runner image. Push it, then set runner_image_digest."
  value       = aws_ecr_repository.runner.repository_url
}

output "cluster_arn" {
  description = "ECS cluster of the runners and of Keycloak."
  value       = aws_ecs_cluster.this.arn
}

output "runner_task_definition_arn" {
  description = "Task definition of the runners. Null before phase 2."
  value       = one(aws_ecs_task_definition.runner[*].arn)
}

output "runner_security_group_id" {
  description = "Security group of the runner tasks."
  value       = aws_security_group.runner.id
}

output "core_autoscaling_group" {
  description = "Auto scaling group of the core. Null before phase 2."
  value       = one(aws_autoscaling_group.core[*].name)
}
