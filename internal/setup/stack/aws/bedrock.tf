# The core calls Amazon Bedrock in this account with the role of its instance. A system
# inference profile sends each request to a foundation model in one of its destination
# regions, so the role needs the profile in this region and the model in each destination.
# GetInferenceProfile, read from this region, names those models: for a us. profile the
# model in each US region that the profile routes to, for a global. profile also the model
# without a region.

locals {
  bedrock_profiles = var.bedrock_models == null ? toset([]) : toset(values(var.bedrock_models))
}

data "aws_bedrock_inference_profile" "model" {
  for_each = local.bedrock_profiles

  inference_profile_id = each.key
}

locals {
  bedrock_resources = sort(distinct(flatten([
    for id, profile in data.aws_bedrock_inference_profile.model : concat(
      ["arn:${local.partition}:bedrock:${local.arn_suffix}:inference-profile/${id}"],
      [for model in profile.models : model.model_arn],
    )
  ])))
}
