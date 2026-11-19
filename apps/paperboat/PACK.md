# The application pack of Paperboat

Paperboat is the example application of S.R.C. The control plane names no application in
its code. It reads what it must know about the application from an application pack: this
directory, with the manifest `pack.json`.

To manage another application, write a pack for it. The package `internal/pack` reads and
checks the manifest. `internal/pack/testdata/harbor` is a second pack, which the tests use
to show that the core does not depend on a name of Paperboat.

## What the pack holds

| Path | Content |
|---|---|
| `pack.json` | The manifest |
| `chart/` | The deployment chart. The platform repository gets it at `<mount>/<chart>` |
| `releases/` | One note `<version>.md` for each release |
| `build.sh` | Builds the images of every release and pushes them to a registry |

## The manifest

| Field | Meaning |
|---|---|
| `schema` | The version of the manifest format. This version reads `1` |
| `name` | The short name. The API uses it as the key of the application in its version lists |
| `title` | The name that a person reads. One to three words |
| `version` | The version of the pack, not of the application |
| `description` | One line about the application |
| `layer` | The platform layer that deploys the application |
| `version_variable` | The variable of the layer with the version to deploy. The layer has an output of the same name with the version that runs |
| `url_output` | The output of the layer with the address of the application |
| `releases.repository` | The repository of the forge with the release notes. Not a repository of the core |
| `releases.directory` | The directory of the notes, in the pack and in that repository |
| `releases.first` | The release that a new environment starts with |
| `chart` | The directory of the chart, inside the pack |
| `mount` | The path of the pack in the platform repository, where the stacks read the chart |
| `build` | The build program, inside the pack |
| `namespace` | The Kubernetes namespace of the application. A debug session reads only there |
| `data_namespace` | The namespace of the data services of the application |
| `deployments` | The deployments that a runbook and the Investigator can name |
| `incident_deployment` | The deployment that an incident names when no model gives a diagnosis |
| `debug_commands` | The kubectl commands of a debug session, with no namespace |
| `health.ready_path` | Answers 200 when the application is ready |
| `health.version_path` | Answers with a JSON object that has the field `version` |
| `health.probe_path` | The request that the monitor repeats to measure the error rate |
| `health.probe_name` | The name of that request in a finding. One or two words |
| `topology.services` | The nodes of the application in the topology view |
| `topology.edges` | Edges between two services, or a service and a node of the platform |

## A pack is not trusted

A pack comes from outside the core. `pack.Load` refuses a manifest when:

- it has a field that the format does not have;
- a name, a path or a version does not match its fixed pattern;
- a path can leave the pack;
- a debug command does not start with `get`, `logs`, `describe` or `top`, has a flag that
  selects a namespace, a cluster or an identity, prints a full object, or reads a secret;
- an edge of the topology joins nodes that do not exist, or two nodes of the platform;
- the chart, the build program or the note of the first release is not in the pack.

The core adds the namespace to each debug command. Only names that passed these checks go
into the prompt of a model, through three placeholders: `{{releases_repository}}`,
`{{releases_directory}}` and `{{deployments}}`.

## Which pack the control plane reads

`clanker` reads the one pack in the directory `apps` of the source files. With more than
one pack there, name one: `clanker up --pack apps/<name>`.

## Not in the pack

- The stacks of the platform (`platform/stacks/*/50-app` and
  `platform/modules/shared/app`) name Paperboat and its variable. A second application
  needs its own application layer.
- The runbooks in `runbooks/jobs` name the deployments and the database of Paperboat.
- The scenes of the simulator are the story of the Paperboat releases 1.1.0, 1.1.1 and
  1.2.0.
- The recorded model answers in `internal/agent/replay` are answers about Paperboat.
- There is no command that validates, imports or evaluates a pack.
