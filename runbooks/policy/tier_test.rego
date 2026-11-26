package clanker.tier_test

import data.clanker.tier
import rego.v1

change(type, actions) := {
	"address": sprintf("%s.x", [type]),
	"type": type,
	"change": {"actions": actions},
}

test_no_changes_is_t0 if {
	d := tier.decision with input as {"kind": "drift", "layer": "50-app", "plan": {"resource_changes": []}}
	d.tier == "T0"
}

test_read_and_noop_are_not_changes if {
	plan := {"resource_changes": [change("helm_release", ["no-op"]), change("kubernetes_secret_v1", ["read"])]}
	d := tier.decision with input as {"kind": "drift", "layer": "50-app", "plan": plan}
	d.tier == "T0"
}

test_app_release_is_t2 if {
	plan := {"resource_changes": [change("helm_release", ["update"])]}
	d := tier.decision with input as {"kind": "app-upgrade", "layer": "50-app", "plan": plan}
	d.tier == "T2"
	d.rule == "normal-change"
}

test_replacing_a_stateful_resource_is_t3 if {
	plan := {"resource_changes": [change("kubernetes_stateful_set_v1", ["delete", "create"])]}
	d := tier.decision with input as {"kind": "change", "layer": "30-data", "plan": plan}
	d.tier == "T3"
	d.rule == "stateful-destroy"
}

test_destroying_a_cloud_database_is_t3 if {
	plan := {"resource_changes": [change("aws_db_instance", ["delete"])]}
	d := tier.decision with input as {"kind": "change", "layer": "30-data", "plan": plan}
	d.tier == "T3"
}

test_updating_a_stateful_resource_in_place_is_t2 if {
	plan := {"resource_changes": [change("kubernetes_config_map_v1", ["update"])]}
	d := tier.decision with input as {"kind": "drift", "layer": "30-data", "plan": plan}
	d.tier == "T2"
}

test_network_layer_change_is_t3 if {
	plan := {"resource_changes": [change("docker_container", ["update"])]}
	d := tier.decision with input as {"kind": "change", "layer": "10-network", "plan": plan}
	d.tier == "T3"
	d.rule == "high-risk-layer"
}

test_identity_change_is_t3 if {
	plan := {"resource_changes": [change("aws_iam_role_policy", ["update"])]}
	d := tier.decision with input as {"kind": "change", "layer": "20-cluster", "plan": plan}
	d.tier == "T3"
	d.rule == "identity-change"
}

test_kubernetes_rbac_change_is_t3 if {
	types := [
		"kubernetes_role_v1", "kubernetes_cluster_role_v1",
		"kubernetes_role_binding_v1", "kubernetes_cluster_role_binding_v1",
		"kubernetes_role", "kubernetes_cluster_role",
		"kubernetes_role_binding", "kubernetes_cluster_role_binding",
	]
	every type in types {
		every actions in [["create"], ["update"], ["delete"]] {
			plan := {"resource_changes": [change(type, actions)]}
			d := tier.decision with input as {"kind": "change", "layer": "40-platform", "plan": plan}
			d.tier == "T3"
			d.rule == "identity-change"
		}
	}
}

test_other_kubernetes_objects_are_t2 if {
	every type in ["kubernetes_service_account_v1", "kubernetes_config_map_v1"] {
		plan := {"resource_changes": [change(type, ["update"])]}
		d := tier.decision with input as {"kind": "change", "layer": "40-platform", "plan": plan}
		d.tier == "T2"
		d.rule == "normal-change"
	}
}

test_unchanged_kubernetes_rbac_is_t0 if {
	plan := {"resource_changes": [change("kubernetes_cluster_role_v1", ["no-op"]), change("kubernetes_role_binding_v1", ["read"])]}
	d := tier.decision with input as {"kind": "drift", "layer": "40-platform", "plan": plan}
	d.tier == "T0"
}

test_blank_slate_is_t3 if {
	plan := {"resource_changes": [change("local_file", ["create"])]}
	d := tier.decision with input as {"kind": "blank-slate", "layer": "50-app", "plan": plan}
	d.tier == "T3"
}

test_database_major_version_is_t3 if {
	before := {"spec": [{"template": [{"spec": [{"container": [{"image": "postgres:17.12-alpine"}]}]}]}]}
	after := {"spec": [{"template": [{"spec": [{"container": [{"image": "postgres:18.1-alpine"}]}]}]}]}
	rc := {"address": "kubernetes_stateful_set_v1.postgres", "type": "kubernetes_stateful_set_v1", "change": {"actions": ["update"], "before": before, "after": after}}
	d := tier.decision with input as {"kind": "postgres-patch", "layer": "30-data", "plan": {"resource_changes": [rc]}}
	d.tier == "T3"
	d.rule == "database-major-version"
}

test_database_minor_version_is_t2 if {
	before := {"spec": [{"template": [{"spec": [{"container": [{"image": "postgres:17.11-alpine"}]}]}]}]}
	after := {"spec": [{"template": [{"spec": [{"container": [{"image": "postgres:17.12-alpine"}]}]}]}]}
	rc := {"address": "kubernetes_stateful_set_v1.postgres", "type": "kubernetes_stateful_set_v1", "change": {"actions": ["update"], "before": before, "after": after}}
	d := tier.decision with input as {"kind": "postgres-patch", "layer": "30-data", "plan": {"resource_changes": [rc]}}
	d.tier == "T2"
}

test_create_only_envelope if {
	creates := {"resource_changes": [change("a", ["create"]), change("b", ["read"])]}
	tier.create_only with input as {"kind": "blank-slate", "layer": "30-data", "plan": creates}

	mixed := {"resource_changes": [change("a", ["create"]), change("b", ["update"])]}
	not tier.create_only with input as {"kind": "blank-slate", "layer": "30-data", "plan": mixed}

	replace := {"resource_changes": [change("a", ["delete", "create"])]}
	not tier.create_only with input as {"kind": "blank-slate", "layer": "30-data", "plan": replace}
}

# A plan of a stack with no resource, as `tofu show -json` prints it: OpenTofu leaves
# resource_changes out.
output_only(action) := {"output_changes": {"network_id": {
	"actions": [action],
	"before": "vpc-0a1b2c3d",
	"after": "vpc-0e0f0a0b",
}}}

test_output_only_plan_of_network_layer_is_t3 if {
	d := tier.decision with input as {"kind": "change", "layer": "10-network", "plan": output_only("update")}
	d.tier == "T3"
	d.rule == "high-risk-layer"
}

test_output_only_plan_of_other_layer_is_t2 if {
	d := tier.decision with input as {"kind": "change", "layer": "30-data", "plan": output_only("create")}
	d.tier == "T2"
	d.rule == "output-change"
	d.reasons == ["outputs changed: network_id. The layers above read them"]
}

test_unchanged_outputs_are_t0 if {
	d := tier.decision with input as {"kind": "drift", "layer": "10-network", "plan": output_only("no-op")}
	d.tier == "T0"
	d.rule == "no-changes"
}

test_output_only_plan_is_inside_the_create_only_envelope if {
	tier.create_only with input as {"kind": "blank-slate", "layer": "20-cluster", "plan": output_only("create")}
}
