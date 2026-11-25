# Runbooks

This directory is the content of the `runbooks` repository in the forge.

| Path | Content |
|---|---|
| `jobs/` | One runbook for each pair of files: `<name>.json` is the definition and `<name>.sh` is the script. This directory is empty here: the runbooks come from the application pack (`apps/paperboat/runbooks/` for the example application), and the control plane puts them in `jobs/` when it seeds the repository |
| `policy/` | The rules that give a plan its risk tier (Rego) |
| `policy/approvals.json` | The number of approvers that a tier T3 change needs |
| `policy/maintenance-windows.json` | The weekly windows in which a tier T3 change can apply. The file is optional and this directory does not have it |
| `notifications/routes.json` | Who is told about which event |

## The approval settings

`policy/approvals.json` has one key:

```json
{"t3_approvers": 2}
```

The value is the number of different approvers that a tier T3 change needs. The allowed
values are 1 and 2. A repository without the file needs two approvers. The control plane
reads the file at the head of `main` each time that it needs the number.

A file that is not valid is an error. This includes text that is not JSON, a key that is
not `t3_approvers`, a value that is not 1 or 2, and data after the object. While the file
is not valid, no work item is approved and the broker issues no apply grant.

Tier T2 always needs one approver. No other tier has a setting.

The control plane copies this directory into the forge only at the first start. Until a
pull request on the `runbooks` repository is possible, set the value before the first
start.

## The maintenance windows

`policy/maintenance-windows.json` has one key, a list of weekly windows:

```json
{"windows": [
  {"days": ["sat", "sun"], "start": "02:00", "minutes": 120, "time_zone": "Europe/Dublin"}
]}
```

| Key | Value |
|---|---|
| `days` | One or more of `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`: the days on which the window starts |
| `start` | The start as `HH:MM` on a 24-hour clock in the time zone |
| `minutes` | The length of the window, from 60 to 1440. A window can cross midnight |
| `time_zone` | An IANA name, for example `Europe/Dublin` or `UTC` |

A repository without the file, or with an empty list, has no windows: an approved tier T3
change applies at once. This directory has no file, so the first deployment does not wait.

With one window or more, an approved tier T3 change applies only inside a window:

- When the last approval comes outside a window, the work item goes to the state
  `Scheduled` and waits for the start of the next window.
- When the last approval comes inside a window with 30 minutes or more left, the change
  applies at once. With less than 30 minutes left, it waits for the next window. The
  apply grant of one layer lasts 30 minutes, and one hour on AWS. The rule is for the
  start of the apply: on AWS the apply of one layer, and anywhere an apply of several
  layers, can end after the window.
- When an approver sets a time for the apply, the change applies at the first moment at or
  after that time that is inside a window with 30 minutes or more left.

The control plane reads the file at the head of `main` when the last approval comes and
again when the apply starts. A change that becomes due outside a window waits for the
next window. This happens when the control plane did not run during the window, or when
the file changed during the wait. The control plane issues the grant when the apply
starts, not at the approval. The runner compares the plan with the state at that time. If
the plan is stale, the work item gets a new plan and waits for approval again.

To cancel a change that waits, an approver or an admin rejects it. No decision starts the
apply before the window.

A file that is not valid is an error. This includes text that is not JSON, a key that is
not in the table, a key that comes twice, a missing key, a value outside the table, and
data after the object. While the file is not valid, no tier T3 work item is approved and
no tier T3 change that waits is applied.

Tiers T0, T1 and T2 do not wait for a maintenance window. The first deployment of an
environment does not wait for one: nothing runs there yet.

The rule that the control plane copies this directory only at the first start applies to
this file too.

## A runbook

A runbook is a routine job that the control plane runs under a standing approval. A person
or a model writes it once. After that, a caller selects it by name and gives values for its
inputs. No caller, human or model, writes a command when the runbook runs.

The definition says what the runbook is. The control plane reads it from the forge at the
head of `main` and checks each request against it before a work item or a grant exists.

```json
{
  "title": "Restart a component",
  "description": "Restart one Paperboat component and wait for the rollout. It changes no data.",
  "tier": "T1",
  "incident_mitigation": true,
  "inputs": {
    "component": {
      "type": "string",
      "description": "The component to restart. Its deployment is in the namespace of the application.",
      "values": ["api", "web", "worker"]
    }
  }
}
```

This is `jobs/restart-component.json`. It has no outputs. `jobs/db-backup.json` has two.

| Field | Meaning |
|---|---|
| `title` | The name of the action, in one to five words of letters, digits and `-`. It goes into the title of the work item of a mitigation: "Restart a component (api) to mitigate WI-0007". Optional, but a mitigation for an incident must have it |
| `description` | What the runbook does and what it does not change. Required |
| `tier` | The risk tier that a run needs: `T0` to `T3`. A standing approval covers only `T1`, so the control plane runs only a `T1` runbook |
| `incident_mitigation` | `true` when the Investigator can propose the runbook for an incident. The default is `false`. The Investigator gives one parameter, `component`, so such a runbook must have a `title` and exactly that input. Each value of the input must be the name of a component of the application pack |
| `inputs` | The parameters, by name. A request must give each one and no other |
| `outputs` | The values that the script reports, by name. A run must report each one and no other |

An input has the `type` `string`, a `description` and `values`, the only values that a
request can give. There is no input of free text. An output has a `type` (`string` or
`integer`) and a `description`.

The name of the runbook is the name of the two files: lower-case letters, digits and `-`.
The name of an input or an output has lower-case letters, digits and `_`.

## The script

The script is the steps of the runbook. The runner starts it with this environment:

| Variable | Content |
|---|---|
| `PARAM_<NAME>` | The value of the input `<name>`, with the name in upper case. A mitigation gets `PARAM_COMPONENT`, the name of a component. The script makes the name of the deployment from it |
| `KUBECONFIG` | The file with the short-lived credential of this run |
| `TARGET_APPLICATION` | The namespace of the application on this environment, for example `paperboat` |
| `TARGET_DATA` | The namespace of the data services of the application, for example `paperboat-data`. It is empty when the data services are not in the cluster, as on AWS, GCP and Azure |
| `OUTPUT_FILE` | The file for the outputs. Append one line `name=value` for each output |
| `STATE_DIR` | The state directory of the environment on the runner |

The two targets come from the output `targets` of the layer `40-platform`, as its last
apply wrote it. A script uses them in place of the names of namespaces. A script that
needs a target stops with a message when that target is empty, before it runs a command:

```bash
data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"
```

The control plane runs no runbook on an environment whose layer `40-platform` has no
output `targets`.

A script that ends with a status other than 0 fails the run, and its outputs are not read.

## What a run records

- The audit event `runbook.succeeded` has the inputs (`params`), the outputs with their
  declared types (`outputs`), the commit of the runbooks repository, the job and the grant.
- The audit event `runbook.failed` has the inputs and the error.
- The log of the job has a line for each input, each target and each output.

A run fails when the script reports an output that the definition does not declare, does
not report a declared output, or reports text for an integer.

## Checks

`go test ./internal/runbook/ ./internal/runner/` reads the runbooks of the example pack. It fails when
a script has no definition, a definition is not valid, or a script does not use an input
or an output of its definition.
