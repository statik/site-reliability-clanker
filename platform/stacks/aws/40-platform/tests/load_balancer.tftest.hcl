# The load balancer of the Envoy Service in 40-platform. All IDs are fakes.

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

run "load_balancer_is_an_internal_nlb_in_the_private_subnets" {
  command = plan

  # The whole map, so that an extra annotation such as aws-load-balancer-scheme, which only
  # the AWS Load Balancer Controller reads, also fails the run.
  assert {
    condition = local.service_annotations == {
      "service.beta.kubernetes.io/aws-load-balancer-type"     = "nlb"
      "service.beta.kubernetes.io/aws-load-balancer-internal" = "true"
      "service.beta.kubernetes.io/aws-load-balancer-subnets"  = "subnet-0000000a,subnet-0000000b"
      "service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags" = join(",", [
        "clanker_env=test-env", "clanker_layer=40-platform", "clanker_ttl=168",
      ])
    }
    error_message = "The Envoy Service must ask for an internal NLB in the private subnets."
  }
}

# The module that waits for the address runs a script with aws and kubectl in an apply, so
# the runs replace it with its output. tests/service_address.tftest.hcl plans the module.
run "public_address_is_the_dns_name_of_the_load_balancer" {
  command = apply

  override_module {
    target  = module.load_balancer
    outputs = { address = "internal-test.elb.example.com" }
  }

  assert {
    condition     = output.public_address == "internal-test.elb.example.com"
    error_message = "public_address must be the DNS name from the Envoy Service."
  }

  assert {
    condition     = output.gateway_name == "clanker" && output.gateway_namespace == "clanker-gateway"
    error_message = "The Gateway is clanker in clanker-gateway."
  }
}

# A later plan reads the Service. A Service that lost its address fails the plan.
run "an_envoy_service_without_an_address_fails_the_plan" {
  command = plan

  override_module {
    target  = module.load_balancer
    outputs = { address = "" }
  }

  expect_failures = [output.public_address]
}
