# The two versioned buckets of the layer go with a test environment when it is destroyed:
# with a TTL, force_destroy removes their objects and versions. Without a TTL it is off.

mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "000000000000" }
  }

  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }

  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }

  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::000000000000:policy/test-boundary" }
  }

  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::000000000000:role/clanker/test-role" }
  }

  mock_resource "aws_s3_bucket" {
    defaults = { arn = "arn:aws:s3:::test-bucket" }
  }
}

variables {
  env_name         = "test-env"
  state_location   = "clanker-test-env-state-000000000000"
  region           = "us-east-1"
  broker_principal = "arn:aws:iam::000000000000:role/test-broker"
}

run "a_test_environment_can_remove_its_buckets" {
  command = plan

  variables {
    ttl_hours = 168
  }

  assert {
    condition     = module.state_bucket.force_destroy && module.evidence_bucket.force_destroy
    error_message = "With a TTL, both buckets must have force_destroy."
  }
}

run "an_environment_without_ttl_keeps_its_buckets" {
  command = plan

  variables {
    ttl_hours = 0
  }

  assert {
    condition     = !module.state_bucket.force_destroy && !module.evidence_bucket.force_destroy
    error_message = "Without a TTL, no bucket may have force_destroy."
  }
}
