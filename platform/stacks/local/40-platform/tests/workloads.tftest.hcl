# The namespace for the pods that the application starts. The runs use mock Kubernetes
# and Helm providers, so nothing is written to a cluster. All values are fakes.

mock_provider "kubernetes" {}
mock_provider "helm" {}

variables {
  env_name                = "local"
  state_location          = "/test-state"
  app_name                = "coder"
  postgres_host           = "postgres.clanker-data.svc.cluster.local"
  postgres_port           = 5432
  postgres_sslmode        = "disable"
  postgres_admin_password = "test-admin-password"
  rwx_storage_class       = "shared-rwx"
  shared_fs_capacity_gb   = 1
  public_address          = "localhost:8088"
}

run "the_targets_name_the_workloads_namespace_of_the_pack" {
  command = apply

  variables {
    workloads_namespace = true
  }

  override_data {
    target = data.terraform_remote_state.data
    values = {
      outputs = {
        postgres_host             = "postgres.clanker-data.svc.cluster.local"
        postgres_port             = 5432
        postgres_sslmode          = "disable"
        postgres_admin_secret_ref = "clanker-data/postgres-admin"
        rwx_storage_class         = "shared-rwx"
        data_namespace            = "clanker-data"
      }
    }
  }

  override_data {
    target = data.kubernetes_secret_v1.postgres_admin
    values = { data = { password = "test-admin-password" } }
  }

  assert {
    condition = output.targets == {
      application = "coder", data = "clanker-data", workloads = "coder-workloads"
    }
    error_message = "The targets must have the namespace <name>-workloads under the key workloads."
  }
}

run "the_workloads_namespace_admits_no_route_and_the_collector_does_not_read_it" {
  command = apply

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    workloads_namespace = true
    collector_endpoint  = "http://host.docker.internal:7070/otlp"
    collector_token     = "test-token"
  }

  assert {
    condition = (
      kubernetes_namespace_v1.workloads[0].metadata[0].name == "coder-workloads"
      && output.workloads_namespace == "coder-workloads"
      && !contains(keys(kubernetes_namespace_v1.workloads[0].metadata[0].labels), "clanker.dev/application")
    )
    error_message = "The namespace <name>-workloads must not have the label that the Gateway admits routes from."
  }

  assert {
    condition     = yamldecode(kubernetes_config_map_v1.collector[0].data["config.yaml"]).receivers.k8s_cluster.namespaces == ["coder"]
    error_message = "The collector must read the namespace of the application only."
  }
}

run "a_pack_without_the_field_gets_no_workloads_namespace" {
  command = apply

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    workloads_namespace = false
  }

  assert {
    condition     = length(kubernetes_namespace_v1.workloads) == 0 && output.workloads_namespace == ""
    error_message = "Without workloads_namespace there must be no second namespace."
  }
}
