locals {
  applier_layers = ["10-network", "20-cluster", "30-data", "40-platform", "50-app"]

  identities = merge(
    {
      observer = "clanker-observer"
      planner  = "clanker-planner"
      debugger = "clanker-debugger"
    },
    { for layer in local.applier_layers : "applier-${layer}" => "clanker-applier-${layer}" },
  )
}

resource "local_file" "evidence_marker" {
  filename = "${var.state_location}/evidence/README.txt"
  content  = "Audit evidence and backups for ${var.env_name}. TTL hours: ${var.ttl_hours}.\n"
}
