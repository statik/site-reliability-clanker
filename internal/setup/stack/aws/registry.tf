# The repository of the runner image. A destroy deletes the images with it.

resource "aws_ecr_repository" "runner" {
  name                 = "${local.name}/runner"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}
