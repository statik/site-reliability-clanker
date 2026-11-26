# The database of the application in modules/shared/platform and the targets of the local
# root. The runs apply with mock Kubernetes and Helm providers, so no Job runs. All values
# are fakes.

mock_provider "kubernetes" {}
mock_provider "helm" {}

variables {
  env_name       = "local"
  state_location = "/test-state"
  app_name       = "paperboat"
}

run "the_local_root_names_everything_after_the_pack" {
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
    condition = (
      output.app_namespace == "paperboat"
      && output.app_secret_name == "paperboat-db"
      && output.shared_volume_claim == "paperboat-shared"
    )
    error_message = "The namespace, the Secret and the claim must have the name of the pack."
  }

  assert {
    condition     = output.targets == { application = "paperboat", data = "clanker-data" }
    error_message = "The targets must be the namespace of the pack and clanker-data."
  }
}

run "the_application_gets_its_own_role_and_never_the_administrator" {
  command = apply

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    app_name                = "harbor"
    postgres_host           = "db.example.com"
    postgres_port           = 5432
    postgres_sslmode        = "require"
    postgres_admin_password = "test-admin-password"
    rwx_storage_class       = "shared-rwx"
    shared_fs_capacity_gb   = 1
    public_address          = "localhost:8088"
  }

  assert {
    condition = (
      kubernetes_secret_v1.database.metadata[0].name == "harbor-db"
      && kubernetes_secret_v1.database.metadata[0].namespace == "harbor"
      && toset(keys(kubernetes_secret_v1.database.data)) == toset([
        "PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE", "DATABASE_URL",
      ])
    )
    error_message = "The Secret <name>-db in the namespace of the pack must have the libpq keys and DATABASE_URL."
  }

  assert {
    condition = (
      kubernetes_secret_v1.database.data["PGUSER"] == "harbor"
      && kubernetes_secret_v1.database.data["PGDATABASE"] == "harbor"
      && kubernetes_secret_v1.database.data["PGSSLMODE"] == "require"
      && kubernetes_secret_v1.database.data["DATABASE_URL"] == format(
        "postgresql://harbor:%s@db.example.com:5432/harbor?sslmode=require",
        random_password.database.result,
      )
    )
    error_message = "The application must connect as the role of the pack to its database, with the sslmode of the target."
  }

  assert {
    condition = alltrue([
      for value in values(kubernetes_secret_v1.database.data) :
      !strcontains(value, "test-admin-password") && !strcontains(value, "postgres:")
    ])
    error_message = "The Secret of the application must not hold the administrator credential."
  }

  assert {
    condition = (
      kubernetes_job_v1.database.metadata[0].namespace == "clanker-platform"
      && can(regex("^harbor-database-[0-9a-f]{10}$", kubernetes_job_v1.database.metadata[0].name))
      && kubernetes_secret_v1.database_job.metadata[0].namespace == "clanker-platform"
      && kubernetes_secret_v1.database_job.data["PGUSER"] == "postgres"
      && kubernetes_secret_v1.database_job.data["PGPASSWORD"] == "test-admin-password"
      && kubernetes_secret_v1.database_job.data["APP_ROLE"] == "harbor"
      && kubernetes_secret_v1.database_job.data["APP_PASSWORD"] == random_password.database.result
    )
    error_message = "The Job and the administrator credential must be in the namespace of the platform."
  }
}

run "an_sslmode_that_drivers_refuse_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    postgres_host           = "db.example.com"
    postgres_port           = 5432
    postgres_sslmode        = "prefer"
    postgres_admin_password = "test-admin-password"
    rwx_storage_class       = "shared-rwx"
    shared_fs_capacity_gb   = 1
  }

  expect_failures = [var.postgres_sslmode]
}

run "an_app_name_that_is_not_a_pack_name_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/platform"
  }

  variables {
    app_name                = "Paper_Boat"
    postgres_host           = "db.example.com"
    postgres_port           = 5432
    postgres_sslmode        = "disable"
    postgres_admin_password = "test-admin-password"
    rwx_storage_class       = "shared-rwx"
    shared_fs_capacity_gb   = 1
  }

  expect_failures = [var.app_name]
}
