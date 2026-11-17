output "role_arns" {
  description = "Map of identity name to role ARN."
  value = merge(
    { for name, role in aws_iam_role.standing : name => role.arn },
    { for name, role in aws_iam_role.gated : name => role.arn },
  )
}

output "boundary_arn" {
  description = "ARN of the permission boundary policy."
  value       = aws_iam_policy.boundary.arn
}

output "workload_role_path" {
  description = "IAM path under which applier-20-cluster can create roles for the cluster."
  value       = local.workload_role_path
}
