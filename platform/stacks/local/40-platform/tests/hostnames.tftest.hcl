# The domain of the host names of the applications, and the DNS of the cluster that answers
# it with the Service of Envoy. The runs use mock Kubernetes and Helm providers, so nothing
# is written to a cluster. All values are fakes.

mock_provider "kubernetes" {}
mock_provider "helm" {}

variables {
  env_name                = "local"
  state_location          = "/test-state"
  app_name                = "paperboat"
  postgres_host           = "postgres.clanker-data.svc.cluster.local"
  postgres_port           = 5432
  postgres_sslmode        = "disable"
  postgres_admin_password = "test-admin-password"
  rwx_storage_class       = "shared-rwx"
  shared_fs_capacity_gb   = 1
  public_address          = "localhost:8088"
}

run "the_local_root_has_the_domain_of_sslip_io_and_the_port_of_the_host" {
  command = apply

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
    condition     = output.app_domain == "127-0-0-1.sslip.io" && output.public_port == 8088
    error_message = "The local target must have the domain 127-0-0-1.sslip.io on the host port 8088."
  }
}

run "the_dns_of_the_cluster_answers_each_name_of_the_domain_with_envoy" {
  command = apply

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    app_domain          = "127-0-0-1.sslip.io"
    rewrite_cluster_dns = true
    public_port         = 8088
  }

  assert {
    condition = (
      kubernetes_config_map_v1_data.coredns[0].metadata[0].namespace == "kube-system"
      && kubernetes_config_map_v1_data.coredns[0].metadata[0].name == "coredns"
      && kubernetes_config_map_v1_data.coredns[0].force
    )
    error_message = "The layer must take over the Corefile of kube-system/coredns."
  }

  assert {
    condition = strcontains(kubernetes_config_map_v1_data.coredns[0].data["Corefile"], join("\n", [
      "    rewrite stop {",
      "       name regex ^(.+\\.)?127-0-0-1\\.sslip\\.io\\.$ clanker-envoy.envoy-gateway-system.svc.cluster.local.",
      "       answer auto",
      "    }",
    ]))
    error_message = "The Corefile must answer every name under the domain, and only those, with the Service of Envoy, under the name of the question."
  }

  assert {
    condition     = output.app_domain == "127-0-0-1.sslip.io" && output.public_port == 8088
    error_message = "The module must give the domain and the port of the listener."
  }
}

run "a_domain_without_the_switch_leaves_the_dns_of_the_cluster" {
  command = apply

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    app_domain = "apps.example.com"
  }

  assert {
    condition     = length(kubernetes_config_map_v1_data.coredns) == 0
    error_message = "Only the switch rewrite_cluster_dns may change the DNS of the cluster."
  }
}

run "a_rewrite_without_a_domain_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    rewrite_cluster_dns = true
  }

  expect_failures = [kubernetes_config_map_v1_data.coredns]
}

run "a_domain_that_is_not_a_domain_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    app_domain = "Apps.Example.COM"
  }

  expect_failures = [var.app_domain]
}
