# Risk tier policy. Input:
#   input.kind    the kind of work item, for example "app-upgrade" or "blank-slate"
#   input.layer   the layer that the plan belongs to
#   input.plan    the output of `tofu show -json`
#
# The result is data.clanker.tier.decision: the tier, the rule that set it and the reasons.
# No model lowers a tier. This file sets the tier. After it, the adversarial review of the
# control plane can make the tier of a work item higher, and never lower.
package clanker.tier

import rego.v1

stateful_types := {
	"kubernetes_stateful_set_v1",
	"kubernetes_persistent_volume_v1",
	"kubernetes_persistent_volume_claim_v1",
	"kubernetes_storage_class_v1",
	"kind_cluster",
	"docker_network",
	"aws_db_instance",
	"aws_efs_file_system",
	"aws_s3_bucket",
	"aws_eks_cluster",
	"aws_vpc",
	"google_sql_database_instance",
	"google_filestore_instance",
	"google_storage_bucket",
	"google_container_cluster",
	"google_compute_network",
	"azurerm_postgresql_flexible_server",
	"azurerm_storage_account",
	"azurerm_storage_share",
	"azurerm_kubernetes_cluster",
	"azurerm_virtual_network",
}

identity_prefixes := ["aws_iam_", "google_project_iam_", "google_service_account", "azurerm_role_"]

high_risk_layers := {"00-bootstrap", "10-network"}

changes contains rc if {
	some rc in input.plan.resource_changes
	some action in rc.change.actions
	action in {"create", "update", "delete"}
}

# The outputs that the plan creates, changes or removes. A plan that changes only outputs is
# applied too: the layers above read the outputs from its state.
output_changes contains name if {
	some name, oc in input.plan.output_changes
	some action in oc.actions
	action in {"create", "update", "delete"}
}

removes(rc) if "delete" in rc.change.actions

is_identity(rc) if {
	some prefix in identity_prefixes
	startswith(rc.type, prefix)
}

t3_reasons contains reason if {
	some rc in changes
	removes(rc)
	rc.type in stateful_types
	reason := {
		"rule": "stateful-destroy",
		"text": sprintf("%s is a stateful resource and the plan destroys or replaces it", [rc.address]),
	}
}

t3_reasons contains reason if {
	count(changes) + count(output_changes) > 0
	input.layer in high_risk_layers
	reason := {
		"rule": "high-risk-layer",
		"text": sprintf("layer %s holds network or identity foundations", [input.layer]),
	}
}

t3_reasons contains reason if {
	some rc in changes
	is_identity(rc)
	reason := {
		"rule": "identity-change",
		"text": sprintf("%s changes who can do what", [rc.address]),
	}
}

t3_reasons contains reason if {
	input.kind in {"blank-slate", "teardown"}
	reason := {
		"rule": "whole-environment",
		"text": "the work item creates or removes a whole environment",
	}
}

t3_reasons contains reason if {
	some rc in changes
	rc.type == "kubernetes_stateful_set_v1"
	"update" in rc.change.actions
	major(rc.change.before) != major(rc.change.after)
	reason := {
		"rule": "database-major-version",
		"text": sprintf("%s changes the database major version", [rc.address]),
	}
}

major(obj) := m if {
	image := obj.spec[0].template[0].spec[0].container[0].image
	tag := split(image, ":")[1]
	m := split(tag, ".")[0]
}

default decision := {"tier": "T0", "rule": "no-changes", "reasons": ["the plan changes nothing"]}

decision := {"tier": "T3", "rule": rule, "reasons": texts} if {
	count(t3_reasons) > 0
	rules := sort([r.rule | some r in t3_reasons])
	rule := rules[0]
	texts := sort([r.text | some r in t3_reasons])
}

decision := {"tier": "T2", "rule": "normal-change", "reasons": [text]} if {
	count(t3_reasons) == 0
	count(changes) > 0
	text := sprintf("resources changed: %d. None is stateful or an identity", [count(changes)])
}

decision := {"tier": "T2", "rule": "output-change", "reasons": [text]} if {
	count(t3_reasons) == 0
	count(changes) == 0
	count(output_changes) > 0
	text := sprintf("outputs changed: %s. The layers above read them", [concat(", ", sort(output_changes))])
}

# A plan is inside the create-only envelope when it only creates resources. The layers of a
# blank-slate deployment that cannot be planned before approval must stay inside it.
default create_only := false

create_only if {
	every rc in changes {
		rc.change.actions == ["create"]
	}
}
