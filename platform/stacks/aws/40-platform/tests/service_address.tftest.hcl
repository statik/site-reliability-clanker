# The module that waits for the load balancer address of the Envoy Service. A plan runs no
# provisioner, so these runs only plan. All names are fakes.

mock_provider "kubernetes" {}

variables {
  cluster_name     = "clanker-test-env"
  cluster_endpoint = "https://cluster.example.com"
  cluster_ca_data  = "dGVzdC1jYQ=="
  region           = "us-east-1"
  service          = "envoy-gateway-system/clanker-envoy"
}

run "the_first_apply_waits_and_then_reads_the_service" {
  command = plan

  module {
    source = "../../../modules/aws/service-address"
  }

  assert {
    condition     = terraform_data.wait.triggers_replace == ["envoy-gateway-system/clanker-envoy"]
    error_message = "The wait must run again only for another Service."
  }

  assert {
    condition = (
      data.kubernetes_service_v1.this.metadata[0].namespace == "envoy-gateway-system"
      && data.kubernetes_service_v1.this.metadata[0].name == "clanker-envoy"
    )
    error_message = "The module must read the Service that it waited for."
  }
}

run "a_service_without_a_namespace_is_refused" {
  command = plan

  module {
    source = "../../../modules/aws/service-address"
  }

  variables {
    service = "clanker-envoy"
  }

  expect_failures = [var.service]
}
