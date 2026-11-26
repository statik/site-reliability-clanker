# The repository of the runner image, and the repository of the images of the application,
# whose prefix is the input image_registry of the layers. A destroy deletes the images with
# each repository.

resource "aws_ecr_repository" "runner" {
  name                 = "${local.name}/runner"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

# platform/stacks/aws/10-network: an image of the application is
# <image_registry>/<application_name>:<version>.
resource "aws_ecr_repository" "application" {
  name                 = "${local.name}/${var.application_name}"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

locals {
  image_registry = "${split("/", aws_ecr_repository.application.repository_url)[0]}/${local.name}"
}
