# The application layer: the local root with the generated app.tf of the example pack, and
# modules/shared/app with the pack in tests/pack, which has values files, an extras chart
# and release records. The runs plan with mock Helm and random providers, so nothing is
# installed. All values are fakes.

mock_provider "helm" {}
mock_provider "random" {}

variables {
  env_name          = "local"
  state_location    = "/test-state"
  paperboat_version = "1.0.0"
  api_replicas      = 1
  worker_replicas   = 2
  web_replicas      = 3
  loadgen_enabled   = false
}

run "the_root_gives_the_module_the_example_pack" {
  command = plan

  override_data {
    target = data.terraform_remote_state.network
    values = { outputs = { image_registry = "localhost:5001" } }
  }

  override_data {
    target = data.terraform_remote_state.platform
    values = {
      outputs = {
        app_namespace       = "paperboat"
        app_secret_name     = "paperboat-db"
        shared_volume_claim = "paperboat-shared"
        gateway_name        = "clanker"
        gateway_namespace   = "clanker-gateway"
        public_address      = "localhost:8088"
        public_port         = 8088
        app_domain          = "127-0-0-1.sslip.io"
      }
    }
  }

  assert {
    condition     = output.app_url == "http://localhost:8088" && output.paperboat_version == "1.0.0"
    error_message = "The URL output and the version output of the pack must come from the module."
  }
}

run "a_version_that_is_not_a_release_is_refused" {
  command = plan

  override_data {
    target = data.terraform_remote_state.network
    values = { outputs = { image_registry = "localhost:5001" } }
  }

  override_data {
    target = data.terraform_remote_state.platform
    values = {
      outputs = {
        app_namespace       = "paperboat"
        app_secret_name     = "paperboat-db"
        shared_volume_claim = "paperboat-shared"
        gateway_name        = "clanker"
        gateway_namespace   = "clanker-gateway"
        public_address      = "localhost:8088"
        public_port         = 8088
        app_domain          = "127-0-0-1.sslip.io"
      }
    }
  }

  variables {
    paperboat_version = "1.0"
  }

  expect_failures = [var.paperboat_version]
}

run "a_setting_outside_its_bounds_is_refused" {
  command = plan

  override_data {
    target = data.terraform_remote_state.network
    values = { outputs = { image_registry = "localhost:5001" } }
  }

  override_data {
    target = data.terraform_remote_state.platform
    values = {
      outputs = {
        app_namespace       = "paperboat"
        app_secret_name     = "paperboat-db"
        shared_volume_claim = "paperboat-shared"
        gateway_name        = "clanker"
        gateway_namespace   = "clanker-gateway"
        public_address      = "localhost:8088"
        public_port         = 8088
        app_domain          = "127-0-0-1.sslip.io"
      }
    }
  }

  variables {
    api_replicas = 6
  }

  expect_failures = [var.api_replicas]
}

run "a_setting_that_is_not_whole_is_refused" {
  command = plan

  override_data {
    target = data.terraform_remote_state.network
    values = { outputs = { image_registry = "localhost:5001" } }
  }

  override_data {
    target = data.terraform_remote_state.platform
    values = {
      outputs = {
        app_namespace       = "paperboat"
        app_secret_name     = "paperboat-db"
        shared_volume_claim = "paperboat-shared"
        gateway_name        = "clanker"
        gateway_namespace   = "clanker-gateway"
        public_address      = "localhost:8088"
        public_port         = 8088
        app_domain          = "127-0-0-1.sslip.io"
      }
    }
  }

  variables {
    web_replicas = 1.5
  }

  expect_failures = [var.web_replicas]
}

run "a_pack_without_values_or_records_gets_its_chart_and_the_platform_values" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "paperboat"
      version         = "1.1.0"
      values          = false
      extras          = false
      records         = false
      timeout_seconds = 300
      secrets         = []
      settings        = { "api.replicas" = "2", "loadgen.enabled" = "false" }
      route           = { service = "paperboat-web", port = 8080, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "local"
    pack_dir            = "../../../../apps/paperboat"
    image_registry      = "localhost:5001"
    app_namespace       = "paperboat"
    app_secret_name     = "paperboat-db"
    shared_volume_claim = "paperboat-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = ""
  }

  assert {
    condition = (
      helm_release.app.name == "paperboat"
      && helm_release.app.chart == "../../../../apps/paperboat/chart"
      && helm_release.app.repository == null
      && helm_release.app.timeout == 300
      && helm_release.app.atomic == false
    )
    error_message = "A pack without records installs its chart directory, with its timeout and without atomic."
  }

  assert {
    condition = (
      length(helm_release.app.values) == 1
      && yamldecode(helm_release.app.values[0]) == {
        platform = {
          name              = "paperboat"
          namespace         = "paperboat"
          version           = "1.1.0"
          url               = "http://localhost:8088"
          hostname          = ""
          imageRegistry     = "localhost:5001"
          databaseSecret    = "paperboat-db"
          sharedVolumeClaim = "paperboat-shared"
          generatedSecret   = ""
        }
      }
    )
    error_message = "The chart must get the values of the platform and nothing else."
  }

  assert {
    condition = { for item in helm_release.app.set : item.name => item.value } == {
      "api.replicas"    = "2"
      "loadgen.enabled" = "false"
    }
    error_message = "Each setting must be one set, with the Helm path as its name."
  }

  assert {
    condition = (
      yamldecode(helm_release.platform.values[0]).route == {
        service = "paperboat-web", port = 8080, paths = [], timeout = "", hostnames = []
      }
      && yamldecode(helm_release.platform.values[0]).platform.APP_VERSION == "1.1.0"
      && length(random_password.generated) == 0
      && length(helm_release.extras) == 0
    )
    error_message = "The route has every path and the default timeout; no Secret and no extras."
  }
}

run "a_pack_gets_its_values_in_order_its_secrets_and_its_extras" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.0.0"
      values          = true
      extras          = true
      records         = true
      timeout_seconds = 600
      secrets         = ["admin-password", "api-key"]
      settings        = { replicaCount = "2" }
      route           = { service = "demo-web", port = 80, paths = ["/api", "/ui"], timeout_seconds = 0, hostname = false, wildcard = false }
    }
    cloud               = "local"
    pack_dir            = "tests/pack"
    image_registry      = "localhost:5001"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = ""
    admin_token         = "test-token"
  }

  assert {
    condition = (
      helm_release.app.chart == "tests/pack/chart"
      && length(helm_release.app.values) == 4
      && yamldecode(helm_release.app.values[0]).source == "values.yaml"
      && yamldecode(helm_release.app.values[1]).source == "values-local.yaml"
      && yamldecode(helm_release.app.values[2]) == { image = { tag = "2.0.0" } }
      && yamldecode(helm_release.app.values[3]).platform.generatedSecret == "demo-generated"
    )
    error_message = "The values must be values.yaml, values-local.yaml, the record, then the platform."
  }

  assert {
    condition     = nonsensitive(helm_release.app.set_sensitive[0].name) == "platform.adminToken"
    error_message = "The admin token must reach the chart as platform.adminToken, with set_sensitive."
  }

  assert {
    condition = (
      length(random_password.generated) == 2
      && random_password.generated["api-key"].length == 32
      && random_password.generated["api-key"].special == false
      && toset([for item in helm_release.platform.set_sensitive : nonsensitive(item.name)]) == toset([
        "generated.admin-password", "generated.api-key",
      ])
    )
    error_message = "Each key of deploy.secrets must get a random value of 32 characters in the Secret <name>-generated."
  }

  assert {
    condition = (
      yamldecode(helm_release.platform.values[0]).route == {
        service = "demo-web", port = 80, paths = ["/api", "/ui"], timeout = "0s", hostnames = []
      }
    )
    error_message = "The route must have the paths of the pack and no request timeout."
  }

  assert {
    condition = (
      helm_release.extras[0].name == "demo-extras"
      && helm_release.extras[0].chart == "tests/pack/extras"
      && helm_release.extras[0].timeout == 600
      && yamldecode(helm_release.extras[0].values[0]).platform.name == "demo"
    )
    error_message = "The extras chart must be the second release, with the values of the platform."
  }
}

run "a_route_with_a_host_name_and_a_wildcard_gets_both_names_and_its_url" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.0.0"
      values          = false
      extras          = true
      records         = false
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = 0, hostname = true, wildcard = true }
    }
    cloud               = "local"
    pack_dir            = "tests/pack"
    image_registry      = "localhost:5001"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = "127-0-0-1.sslip.io"
  }

  assert {
    condition     = output.url == "http://demo.127-0-0-1.sslip.io:8088"
    error_message = "The URL must be the host name of the pack on the domain, with the public port."
  }

  assert {
    condition = (
      yamldecode(helm_release.platform.values[0]).route.hostnames == [
        "demo.127-0-0-1.sslip.io", "*.demo.127-0-0-1.sslip.io",
      ]
      && yamldecode(helm_release.platform.values[0]).platform == {
        APP_URL           = "http://demo.127-0-0-1.sslip.io:8088"
        APP_HOSTNAME      = "demo.127-0-0-1.sslip.io"
        APP_WILDCARD_HOST = "*.demo.127-0-0-1.sslip.io"
        APP_VERSION       = "2.0.0"
      }
    )
    error_message = "The route must have the host name and the wildcard, and the ConfigMap must have them too."
  }

  assert {
    condition = (
      yamldecode(helm_release.app.values[0]).platform.hostname == "demo.127-0-0-1.sslip.io"
      && yamldecode(helm_release.app.values[0]).platform.url == "http://demo.127-0-0-1.sslip.io:8088"
      && yamldecode(helm_release.extras[0].values[0]).platform.hostname == "demo.127-0-0-1.sslip.io"
    )
    error_message = "The charts of the pack must get the host name and the URL in the values of the platform."
  }
}

run "a_route_with_a_host_name_and_no_wildcard_gets_one_name" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.0.0"
      values          = false
      extras          = false
      records         = false
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = true, wildcard = false }
    }
    cloud               = "local"
    pack_dir            = "tests/pack"
    image_registry      = "localhost:5001"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = "127-0-0-1.sslip.io"
  }

  assert {
    condition = (
      yamldecode(helm_release.platform.values[0]).route.hostnames == ["demo.127-0-0-1.sslip.io"]
      && yamldecode(helm_release.platform.values[0]).platform.APP_WILDCARD_HOST == ""
    )
    error_message = "A route without a wildcard must have only the host name of the pack."
  }
}

run "a_host_name_without_a_domain_keeps_the_public_address" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.0.0"
      values          = false
      extras          = false
      records         = false
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = true, wildcard = true }
    }
    cloud               = "aws"
    pack_dir            = "tests/pack"
    image_registry      = "registry.example.com/demo"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "demo.example.com"
    public_port         = 80
    app_domain          = ""
  }

  assert {
    condition = (
      output.url == "http://demo.example.com"
      && yamldecode(helm_release.platform.values[0]).route.hostnames == []
      && yamldecode(helm_release.platform.values[0]).platform.APP_HOSTNAME == ""
      && yamldecode(helm_release.app.values[0]).platform.hostname == ""
    )
    error_message = "Without app_domain the route has no host name and the URL is the public address."
  }
}

run "a_record_with_an_archive_installs_the_archive" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.1.0"
      values          = false
      extras          = false
      records         = true
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "aws"
    pack_dir            = "tests/pack"
    image_registry      = "registry.example.com/demo"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "demo.example.com"
    public_port         = 8088
    app_domain          = ""
    admin_token         = "test-token"
  }

  assert {
    condition = (
      helm_release.app.chart == "tests/pack/charts/demo-2.1.0.tgz"
      && helm_release.app.repository == null
    )
    error_message = "A record with an archive must install the archive of the pack."
  }

  assert {
    condition = (
      length(helm_release.app.values) == 0
      && length(helm_release.app.set_sensitive) == 0
    )
    error_message = "A chart of another project must get neither the platform values nor the admin token."
  }
}

run "a_record_with_an_oci_chart_pins_the_digest" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.2.0"
      values          = false
      extras          = false
      records         = true
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "gcp"
    pack_dir            = "tests/pack"
    image_registry      = "registry.example.com/demo"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "demo.example.com"
    public_port         = 8088
    app_domain          = ""
  }

  assert {
    condition = (
      helm_release.app.chart == "oci://ghcr.io/example/charts/demo@sha256:455d6a04fe43df4a93b40e1f92d65b2a9befaa0348e5a461295d961161ebf475"
      && helm_release.app.version == "2.2.0"
      && helm_release.app.repository == null
    )
    error_message = "An OCI chart must be <repository>/<name>@<digest> with the version, so Helm checks both."
  }

  assert {
    condition     = length(helm_release.app.values) == 0
    error_message = "An OCI chart of another project must get no platform values."
  }
}

run "a_record_with_an_https_chart_names_the_repository" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.3.0"
      values          = false
      extras          = false
      records         = true
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "azure"
    pack_dir            = "tests/pack"
    image_registry      = "registry.example.com/demo"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "demo.example.com"
    public_port         = 8088
    app_domain          = ""
  }

  assert {
    condition = (
      helm_release.app.repository == "https://charts.example.com"
      && helm_release.app.chart == "demo"
      && helm_release.app.version == "2.3.0"
    )
    error_message = "A chart in an https repository must be the repository, the name and the version."
  }

  assert {
    condition     = length(helm_release.app.values) == 0
    error_message = "A chart of another project in an https repository must get no platform values."
  }
}

run "an_archive_with_another_sha256_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.4.0"
      values          = false
      extras          = false
      records         = true
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "local"
    pack_dir            = "tests/pack"
    image_registry      = "localhost:5001"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = ""
  }

  expect_failures = [helm_release.app]
}

run "a_version_without_a_record_is_refused" {
  command = plan

  module {
    source = "../../../modules/shared/app"
  }

  variables {
    app = {
      name            = "demo"
      version         = "2.9.0"
      values          = false
      extras          = false
      records         = true
      timeout_seconds = 300
      secrets         = []
      settings        = {}
      route           = { service = "demo-web", port = 80, paths = [], timeout_seconds = null, hostname = false, wildcard = false }
    }
    cloud               = "local"
    pack_dir            = "tests/pack"
    image_registry      = "localhost:5001"
    app_namespace       = "demo"
    app_secret_name     = "demo-db"
    shared_volume_claim = "demo-shared"
    gateway_name        = "clanker"
    gateway_namespace   = "clanker-gateway"
    public_address      = "localhost:8088"
    public_port         = 8088
    app_domain          = ""
  }

  expect_failures = [helm_release.app]
}
