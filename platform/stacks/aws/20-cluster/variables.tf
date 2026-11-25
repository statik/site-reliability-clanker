variable "env_name" {
  description = "Environment name, for example aws-sandbox."
  type        = string
}

variable "state_location" {
  description = "Name of the S3 bucket that holds OpenTofu state."
  type        = string
}

variable "region" {
  description = "AWS region of the environment."
  type        = string
}

variable "kubernetes_version" {
  description = "Kubernetes minor version, for example 1.36."
  type        = string
}

variable "node_image_version" {
  description = "EKS AMI release version for the nodes. Empty means the latest for the version."
  type        = string
}

variable "node_count" {
  description = "Number of worker nodes."
  type        = number
}

variable "node_size" {
  description = "EC2 instance type of the worker nodes. Its architecture must match node_ami_type."
  type        = string
  default     = "t4g.medium"
}

variable "node_ami_type" {
  description = "EKS AMI type: AL2023_ARM_64_STANDARD (Graviton) or AL2023_x86_64_STANDARD."
  type        = string
  default     = "AL2023_ARM_64_STANDARD"
}

variable "instance_tags" {
  description = <<-EOT
    Tags that the organization requires on each EC2 instance launch. The node launch
    template puts them on each node instance and its volumes. The owner gives the values
    at run time. Never write them into a file in the public repository. Each value is
    marked sensitive, so the scrubber of the control plane learns it from the plan.
  EOT
  type        = map(string)
  default     = {}
}

variable "operator_principal_arn" {
  description = <<-EOT
    ARN of the IAM role or IAM user of the operator, for example the role that the owner
    signs in with. It becomes a cluster admin, so that the operator can remove the layers
    above this one with their own credentials. Give the role ARN
    (arn:aws:iam::<account>:role/<path><name>), not a session ARN
    (arn:aws:sts::<account>:assumed-role/...). Do not give the role that applies this
    layer: EKS makes that role a cluster admin already, and a second entry fails.
    Empty: no entry.
  EOT
  type        = string
  default     = ""

  validation {
    condition = (
      var.operator_principal_arn == ""
      || can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:(role|user)/.+$", var.operator_principal_arn))
    )
    error_message = "operator_principal_arn must be empty or an IAM role or user ARN (not STS)."
  }
}
