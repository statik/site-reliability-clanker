locals {
  layers = ["10-network", "20-cluster", "30-data", "40-platform", "50-app"]

  observer_roles = [
    "roles/artifactregistry.reader",
    "roles/cloudsql.viewer",
    "roles/compute.viewer",
    "roles/container.viewer",
    "roles/file.viewer",
    "roles/logging.viewer",
    "roles/monitoring.viewer",
  ]

  # Roles with standing access. None of them can change a resource.
  standing_roles = {
    observer = local.observer_roles
    planner = concat(local.observer_roles, [
      "roles/iam.securityReviewer",
      "roles/secretmanager.secretAccessor",
      "roles/secretmanager.viewer",
      "roles/storage.bucketViewer",
    ])
  }

  # Roles that IAM grants only inside a grant window.
  elevated_roles = {
    "debugger" = [
      "roles/container.clusterViewer",
    ]
    "applier-10-network" = [
      "roles/artifactregistry.admin",
      "roles/compute.networkAdmin",
      "roles/servicenetworking.networksAdmin",
    ]
    "applier-20-cluster" = [
      "roles/compute.viewer",
      "roles/container.admin",
    ]
    "applier-30-data" = [
      "roles/cloudsql.admin",
      "roles/compute.networkViewer",
      "roles/container.admin",
      "roles/file.editor",
      "roles/secretmanager.admin",
      "roles/storage.admin",
    ]
    "applier-40-platform" = [
      "roles/container.admin",
      "roles/secretmanager.secretAccessor",
    ]
    "applier-50-app" = [
      "roles/container.developer",
    ]
  }

  appliers = [for layer in local.layers : "applier-${layer}"]

  identity_names = concat(keys(local.standing_roles), keys(local.elevated_roles))

  standing_bindings = merge([
    for identity, roles in local.standing_roles : {
      for role in roles : "${identity}/${role}" => { identity = identity, role = role }
    }
  ]...)

  elevated_bindings = merge([
    for identity, roles in local.elevated_roles : {
      for role in roles : "${identity}/${role}" => { identity = identity, role = role }
    }
  ]...)

  # A window that starts and ends at the same instant never matches a request.
  closed_window = {
    not_before = "1970-01-01T00:00:00Z"
    expires    = "1970-01-01T00:00:00Z"
  }

  window_expressions = {
    for identity in keys(local.elevated_roles) : identity => format(
      "request.time >= timestamp(\"%s\") && request.time < timestamp(\"%s\")",
      lookup(var.grant_windows, identity, local.closed_window).not_before,
      lookup(var.grant_windows, identity, local.closed_window).expires,
    )
  }

  denied_permissions = [
    "cloudresourcemanager.googleapis.com/projects.delete",
    "cloudresourcemanager.googleapis.com/projects.setIamPolicy",
    "iam.googleapis.com/roles.create",
    "iam.googleapis.com/roles.delete",
    "iam.googleapis.com/roles.update",
    "iam.googleapis.com/serviceAccountKeys.create",
    "iam.googleapis.com/serviceAccounts.create",
    "iam.googleapis.com/serviceAccounts.delete",
    "iam.googleapis.com/serviceAccounts.getAccessToken",
    "iam.googleapis.com/serviceAccounts.getOpenIdToken",
    "iam.googleapis.com/serviceAccounts.implicitDelegation",
    "iam.googleapis.com/serviceAccounts.setIamPolicy",
    "iam.googleapis.com/serviceAccounts.signBlob",
    "iam.googleapis.com/serviceAccounts.signJwt",
  ]
}

resource "google_service_account" "agent" {
  for_each = toset(local.identity_names)

  project      = var.project_id
  account_id   = "clanker-${each.key}"
  display_name = "Clanker ${each.key}"
}

# The binding is authoritative for the role, so no other member can mint tokens.
resource "google_service_account_iam_binding" "broker_token_creator" {
  for_each = toset(var.broker_principal == "" ? [] : local.identity_names)

  service_account_id = google_service_account.agent[each.key].name
  role               = "roles/iam.serviceAccountTokenCreator"
  members            = [var.broker_principal]
}

resource "google_project_iam_member" "standing" {
  for_each = local.standing_bindings

  project = var.project_id
  role    = each.value.role
  member  = google_service_account.agent[each.value.identity].member
}

resource "google_storage_bucket_iam_member" "planner_state" {
  bucket = var.state_bucket
  role   = "roles/storage.objectViewer"
  member = google_service_account.agent["planner"].member
}

resource "google_project_iam_member" "elevated" {
  for_each = local.elevated_bindings

  project = var.project_id
  role    = each.value.role
  member  = google_service_account.agent[each.value.identity].member

  condition {
    title       = "clanker-grant-window"
    description = "Usable only inside the grant window of ${each.value.identity}."
    expression  = local.window_expressions[each.value.identity]
  }
}

resource "google_storage_bucket_iam_member" "applier_state" {
  for_each = toset(local.appliers)

  bucket = var.state_bucket
  role   = "roles/storage.objectUser"
  member = google_service_account.agent[each.key].member

  condition {
    title       = "clanker-grant-window"
    description = "Usable only inside the grant window of ${each.key}."
    expression  = local.window_expressions[each.key]
  }
}

# GKE nodes run as this account. It is not an agent identity and no token is minted for it.
resource "google_service_account" "node" {
  project      = var.project_id
  account_id   = "clanker-node"
  display_name = "Clanker GKE nodes"
}

resource "google_project_iam_member" "node" {
  for_each = toset([
    "roles/artifactregistry.reader",
    "roles/container.defaultNodeServiceAccount",
  ])

  project = var.project_id
  role    = each.key
  member  = google_service_account.node.member
}

resource "google_service_account_iam_member" "cluster_applier_uses_node" {
  service_account_id = google_service_account.node.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.agent["applier-20-cluster"].member

  condition {
    title       = "clanker-grant-window"
    description = "Usable only inside the grant window of applier-20-cluster."
    expression  = local.window_expressions["applier-20-cluster"]
  }
}

resource "google_iam_deny_policy" "guard" {
  parent       = urlencode("cloudresourcemanager.googleapis.com/projects/${var.project_id}")
  name         = "clanker-agent-guard"
  display_name = "Clanker agent identities cannot change IAM or mint credentials"

  rules {
    description = "No agent identity can change IAM, make keys or impersonate an account."

    deny_rule {
      denied_principals = [
        for account in google_service_account.agent :
        "principal://iam.googleapis.com/projects/-/serviceAccounts/${account.email}"
      ]
      denied_permissions = local.denied_permissions
    }
  }
}
