# 10-network with a new VPC and in existing-network mode. All IDs are fakes.

# The mock puts each subnet in the network vpc-00000000.
mock_provider "aws" {
  mock_data "aws_availability_zones" {
    defaults = { names = ["zone-a", "zone-b", "zone-c"] }
  }

  mock_data "aws_subnet" {
    defaults = { vpc_id = "vpc-00000000" }
  }
}

override_data {
  target = data.terraform_remote_state.bootstrap
  values = { outputs = { clanker_ttl = "168" } }
}

variables {
  env_name       = "test-env"
  state_location = "test-state"
  region         = "us-east-1"
  network_cidr   = "10.40.0.0/16"
  zone_count     = 2
  image_registry = "registry.example.com/test-env"
}

run "new_network_without_existing_ids" {
  command = plan

  assert {
    condition     = length(module.network) == 1
    error_message = "Without existing_network_id the layer must make a new VPC."
  }

  assert {
    condition     = output.image_registry == "registry.example.com/test-env"
    error_message = "image_registry must pass the input through."
  }
}

run "existing_network_makes_no_vpc_nat_or_gateway" {
  command = plan

  variables {
    existing_network_id         = "vpc-00000000"
    existing_private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"]
  }

  # The VPC, the NAT gateway, the internet gateway and the route tables are all in
  # module.network, so no instance of it means none of them.
  assert {
    condition     = length(module.network) == 0
    error_message = "With an existing network the layer must make no VPC, NAT or internet gateway."
  }

  assert {
    condition     = output.network_id == "vpc-00000000"
    error_message = "network_id must be existing_network_id."
  }

  assert {
    condition     = output.private_subnet_ids == tolist(["subnet-0000000a", "subnet-0000000b"])
    error_message = "private_subnet_ids must be existing_private_subnet_ids, in the same order."
  }

  assert {
    condition     = length(output.public_subnet_ids) == 0
    error_message = "public_subnet_ids must be empty in existing-network mode."
  }

  assert {
    condition     = output.image_registry == "registry.example.com/test-env"
    error_message = "image_registry must pass the input through."
  }
}

run "subnet_of_another_network_is_refused" {
  command = plan

  variables {
    existing_network_id         = "vpc-0000000f"
    existing_private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"]
  }

  expect_failures = [data.aws_subnet.existing]
}

run "one_existing_subnet_is_refused" {
  command = plan

  variables {
    existing_network_id         = "vpc-00000000"
    existing_private_subnet_ids = ["subnet-0000000a"]
  }

  expect_failures = [var.existing_private_subnet_ids]
}

run "existing_subnets_without_a_network_are_refused" {
  command = plan

  variables {
    existing_private_subnet_ids = ["subnet-0000000a", "subnet-0000000b"]
  }

  expect_failures = [var.existing_private_subnet_ids]
}

run "registry_with_a_scheme_is_refused" {
  command = plan

  variables {
    image_registry = "https://registry.example.com"
  }

  expect_failures = [var.image_registry]
}
