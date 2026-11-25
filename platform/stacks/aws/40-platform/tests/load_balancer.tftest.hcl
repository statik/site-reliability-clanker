# The annotations of the Envoy Service in 40-platform. All IDs are fakes.

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
      cluster_name           = "clanker-test-env"
      cluster_endpoint       = "https://cluster.example.com"
      cluster_ca_certificate = "test-ca"
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

override_module {
  target = module.platform
  outputs = {
    app_namespace       = "paperboat"
    gateway_class       = "envoy"
    public_address      = "internal-test.elb.example.com"
    app_secret_name     = "paperboat-db"
    shared_volume_claim = "paperboat-shared"
    collector_namespace = ""
  }
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
