terraform {
  required_version = ">= 1.10"

  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = "3.3.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
  }
}

# The release record of the version, when the pack has records. A pack with records has one
# for every release; the precondition of the main release stops a version without one.
locals {
  name        = var.app.name
  version     = var.app.version
  record_path = "apps/${local.name}/releases/${local.version}.json"
  record_file = "${var.pack_dir}/releases/${local.version}.json"
  has_record  = var.app.records && fileexists(local.record_file)
  record      = local.has_record ? jsondecode(file(local.record_file)) : null

  # The chart of the release: an archive of the pack, a chart in a repository, or the chart
  # directory of the pack. Helm checks the digest of an OCI chart against its version; it
  # does not check the digest of a chart in an https repository.
  archive    = try(local.record.chart, "")
  repository = try(local.record.chart_repository, "")
  oci        = startswith(local.repository, "oci://")
  chart = (
    local.archive != "" ? "${var.pack_dir}/${local.archive}" :
    local.oci ? "${local.repository}/${local.record.chart_name}@${local.record.chart_digest}" :
    local.repository != "" ? local.record.chart_name :
    "${var.pack_dir}/chart"
  )
  # The chart of a record is a chart of another project. Its values schema can refuse a key
  # that it does not know, as the schema of Zero to JupyterHub does, so it gets no values of
  # the platform. The values files of the pack connect it to the objects of the platform.
  own_chart = local.archive == "" && local.repository == ""

  url              = "http://${var.public_address}"
  generated_secret = length(var.app.secrets) > 0 ? "${local.name}-generated" : ""

  # The values that the main chart of the pack and its extras chart get from the platform
  # (platform/CONTRACT.md section 4). adminToken comes with set_sensitive.
  platform_values = {
    name              = local.name
    namespace         = var.app_namespace
    version           = local.version
    url               = local.url
    hostname          = ""
    imageRegistry     = var.image_registry
    databaseSecret    = var.app_secret_name
    sharedVolumeClaim = var.shared_volume_claim
    generatedSecret   = local.generated_secret
  }

  values_dir  = "${var.pack_dir}/values"
  cloud_value = "${local.values_dir}/values-${var.cloud}.yaml"

  # Lowest first: values.yaml, values-<cloud>.yaml, the values of the record, the values
  # of the platform for a chart of the pack. The settings come last, with set.
  main_values = concat(
    var.app.values ? [file("${local.values_dir}/values.yaml")] : [],
    var.app.values && fileexists(local.cloud_value) ? [file(local.cloud_value)] : [],
    try(local.record.values, null) != null ? [yamlencode(local.record.values)] : [],
    local.own_chart ? [yamlencode({ platform = local.platform_values })] : [],
  )
  admin_token = var.admin_token == "" ? [] : [{
    name  = "platform.adminToken"
    value = var.admin_token
  }]
}

resource "random_password" "generated" {
  for_each = toset(var.app.secrets)

  length  = 32
  special = false
}

# The objects of the platform for the pack. They come first: a chart can read the ConfigMap
# and the Secret when its pods start.
resource "helm_release" "platform" {
  name      = "${local.name}-platform"
  namespace = var.app_namespace
  chart     = "${path.module}/platform"

  values = [yamlencode({
    name = local.name
    gateway = {
      name      = var.gateway_name
      namespace = var.gateway_namespace
    }
    route = {
      service = var.app.route.service
      port    = var.app.route.port
      paths   = var.app.route.paths
      timeout = var.app.route.timeout_seconds == null ? "" : "${var.app.route.timeout_seconds}s"
    }
    platform = {
      APP_URL           = local.url
      APP_HOSTNAME      = ""
      APP_WILDCARD_HOST = ""
      APP_VERSION       = local.version
    }
  })]

  set_sensitive = [for key in var.app.secrets : {
    name  = "generated.${key}"
    value = random_password.generated[key].result
    type  = "string"
  }]
}

resource "helm_release" "app" {
  name       = local.name
  namespace  = var.app_namespace
  repository = local.oci || local.repository == "" ? null : local.repository
  chart      = local.chart
  version    = local.repository == "" ? null : local.record.chart_version

  wait          = true
  wait_for_jobs = true
  # Helm never rolls back by itself: a release can have migrated the database.
  atomic  = false
  timeout = var.app.timeout_seconds

  values = local.main_values
  set = [for path, value in var.app.settings : {
    name  = path
    value = value
  }]
  set_sensitive = local.own_chart ? local.admin_token : []

  lifecycle {
    precondition {
      condition     = !var.app.records || local.record != null
      error_message = "Release ${local.version} has no record ${local.record_path}. Add the record of the release, and its chart archive when it names one, in a pull request on the platform repository."
    }
    precondition {
      condition     = local.archive == "" || try(filesha256("${var.pack_dir}/${local.archive}") == local.record.chart_sha256, false)
      error_message = "The chart archive ${local.archive} of release ${local.version} is missing, or its SHA-256 is not the chart_sha256 of ${local.record_path}."
    }
  }

  depends_on = [helm_release.platform]
}

# The second chart of the pack, for objects that the main chart does not have.
resource "helm_release" "extras" {
  count = var.app.extras ? 1 : 0

  name      = "${local.name}-extras"
  namespace = var.app_namespace
  chart     = "${var.pack_dir}/extras"

  wait          = true
  wait_for_jobs = true
  atomic        = false
  timeout       = var.app.timeout_seconds

  values        = [yamlencode({ platform = local.platform_values })]
  set_sensitive = local.admin_token

  depends_on = [helm_release.app]
}
