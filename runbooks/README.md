# Runbooks

This directory is the content of the `runbooks` repository in the forge.

| Path | Content |
|---|---|
| `jobs/` | One runbook for each pair of files: `<name>.json` is the definition and `<name>.sh` is the script |
| `policy/` | The rules that give a plan its risk tier (Rego) |
| `notifications/routes.json` | Who is told about which event |

## A runbook

A runbook is a routine job that the control plane runs under a standing approval. A person
or a model writes it once. After that, a caller selects it by name and gives values for its
inputs. No caller, human or model, writes a command when the runbook runs.

The definition says what the runbook is. The control plane reads it from the forge at the
head of `main` and checks each request against it before a work item or a grant exists.

```json
{
  "description": "Restart one Paperboat deployment and wait for the rollout. It changes no data.",
  "tier": "T1",
  "incident_mitigation": true,
  "inputs": {
    "deployment": {
      "type": "string",
      "description": "The deployment to restart, in the namespace paperboat.",
      "values": ["paperboat-api", "paperboat-web", "paperboat-worker"]
    }
  }
}
```

This is `jobs/restart-deployment.json`. It has no outputs. `jobs/db-backup.json` has two.

| Field | Meaning |
|---|---|
| `description` | What the runbook does and what it does not change. Required |
| `tier` | The risk tier that a run needs: `T0` to `T3`. A standing approval covers only `T1`, so the control plane runs only a `T1` runbook |
| `incident_mitigation` | `true` when the Investigator can propose the runbook for an incident. The default is `false`. The Investigator gives one parameter, `deployment`, so such a runbook must have exactly that input |
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
| `PARAM_<NAME>` | The value of the input `<name>`, with the name in upper case |
| `KUBECONFIG` | The file with the short-lived credential of this run |
| `OUTPUT_FILE` | The file for the outputs. Append one line `name=value` for each output |
| `STATE_DIR` | The state directory of the environment on the runner |

A script that ends with a status other than 0 fails the run, and its outputs are not read.

## What a run records

- The audit event `runbook.succeeded` has the inputs (`params`), the outputs with their
  declared types (`outputs`), the commit of the runbooks repository, the job and the grant.
- The audit event `runbook.failed` has the inputs and the error.
- The log of the job has a line for each input and each output.

A run fails when the script reports an output that the definition does not declare, does
not report a declared output, or reports text for an integer.

## Checks

`go test ./internal/runbook/ ./internal/runner/` reads the files in `jobs/`. It fails when
a script has no definition, a definition is not valid, or a script does not use an input
or an output of its definition.
