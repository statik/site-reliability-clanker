output "name" {
  description = "Bucket name."
  value       = aws_s3_bucket.this.id
}

output "arn" {
  description = "Bucket ARN."
  value       = aws_s3_bucket.this.arn
}

output "force_destroy" {
  description = "Whether a destroy removes the bucket with its objects and versions."
  value       = aws_s3_bucket.this.force_destroy
}
