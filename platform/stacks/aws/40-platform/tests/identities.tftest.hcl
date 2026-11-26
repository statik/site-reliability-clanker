# The rights of the runbook identity in 40-platform on AWS. All IDs are fakes.

mock_provider "aws" {}
mock_provider "kubernetes" {}
mock_provider "helm" {}

override_data {
  target = data.terraform_remote_state.bootstrap
  values = { outputs = { clanker_ttl = "168" } }
}

override_data {
  target = data.terraform_remote_state.network
  values = { outputs = { private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"] } }
}

override_data {
  target = data.terraform_remote_state.cluster
  values = {
    outputs = {
      cluster_name                       = "clanker-test-env"
      cluster_endpoint                   = "https://cluster.example.com"
      cluster_ca_certificate             = "test-ca"
      cluster_certificate_authority_data = "dGVzdC1jYQ=="
    }
  }
}

override_data {
  target = data.terraform_remote_state.data
  values = {
    outputs = {
      postgres_host             = "db.example.com"
      postgres_port             = 5432
      postgres_database         = "paperboat"
      postgres_admin_secret_ref = "test-secret"
      rwx_storage_class         = "efs-rwx"
      shared_fs_capacity_gb     = 5
    }
  }
}

override_data {
  target = data.aws_secretsmanager_secret_version.postgres_admin
  values = { secret_string = "{\"password\":\"test-password\"}" }
}

variables {
  env_name       = "test-env"
  state_location = "test-state"
  region         = "us-east-1"
}

run "the_runbook_group_has_the_rights_of_the_local_runbook_identity" {
  command = plan

  assert {
    condition = (
      kubernetes_role_v1.runbook.metadata[0].namespace == "paperboat"
      && length(kubernetes_role_v1.runbook.rule) == 2
      && kubernetes_role_v1.runbook.rule[0].api_groups == toset(["apps"])
      && kubernetes_role_v1.runbook.rule[0].resources == toset(["deployments"])
      && kubernetes_role_v1.runbook.rule[0].verbs == toset(["get", "list", "patch"])
      && kubernetes_role_v1.runbook.rule[1].api_groups == toset([""])
      && kubernetes_role_v1.runbook.rule[1].resources == toset(["pods", "pods/log"])
      && kubernetes_role_v1.runbook.rule[1].verbs == toset(["get", "list"])
    )
    error_message = "The runbook role must have the rules of the local target, in the application namespace."
  }

  assert {
    condition = (
      kubernetes_role_binding_v1.runbook.metadata[0].namespace == "paperboat"
      && kubernetes_role_binding_v1.runbook.role_ref[0].kind == "Role"
      && kubernetes_role_binding_v1.runbook.role_ref[0].name == "clanker-runbook"
      && length(kubernetes_role_binding_v1.runbook.subject) == 1
      && kubernetes_role_binding_v1.runbook.subject[0].kind == "Group"
      && kubernetes_role_binding_v1.runbook.subject[0].name == "clanker-runbook"
    )
    error_message = "Only the group clanker-runbook of 20-cluster may get the runbook role."
  }
}
