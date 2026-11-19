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
| `runbooks/` | The runbooks of the application: `<name>.json` and `<name>.sh` for each |
| `evals/` | The eval cases of the application: `<task>.json` for a task of the eval suite |

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
| `runbooks` | Optional. The directory of the runbooks, inside the pack |
| `evals` | Optional. The directory of the eval cases, inside the pack |

## A pack is not trusted

A pack comes from outside the core. `pack.Load` refuses a manifest when:

- it has a field that the format does not have;
- a name, a path or a version does not match its fixed pattern;
- a path can leave the pack;
- a debug command does not start with `get`, `logs`, `describe` or `top`, has a flag that
  selects a namespace, a cluster or an identity, prints a full object, or reads a secret;
- an edge of the topology joins nodes that do not exist, or two nodes of the platform;
- the chart, the build program or the note of the first release is not in the pack;
- the directory of the runbooks or of the eval cases has a directory in it, a file of
  more than 256 KiB, a script with no definition, a definition with no script or a
  definition that `internal/runbook` refuses;
- a runbook that the Investigator can propose names a deployment that is not in
  `deployments`;
- an eval case file does not have `schema` 1, the task of its name, and 1 to 200 samples
  that each have their own `id`.

The core adds the namespace to each debug command. Only names that passed these checks go
into the prompt of a model, through three placeholders: `{{releases_repository}}`,
`{{releases_directory}}` and `{{deployments}}`.

## Runbooks

The runbooks of an application are in the pack, because they name its deployments and its
database. At the first start, the control plane puts them in the directory `jobs/` of the
runbooks repository, beside the policies and the notification routes of the core. A pack
can put files only there: it cannot bring a policy, change a risk tier or add a route.
`runbooks/README.md` has the format of a definition.

## Eval cases

`evals/investigator_diagnosis.json` has the incidents that the eval suite gives to the
Investigator role: for each one the transcript of the debug session, the true cause, the
mitigation that is acceptable, and the evidence that the answer must cite. The suite
(`evals/`) has the checks, which are the same for every pack. Only this task reads its
samples from the pack; the other tasks have their samples in the code of the suite.

```sh
cd evals
./run.sh pack ../apps/paperboat        # the tasks with cases in the pack, with mockllm/model
./run.sh pack ../apps/paperboat live   # the same tasks with the live models
```

The logs go to `evals/logs/pack-<directory>-<mode>/`.

## Commands

`clanker pack validate DIR` checks the pack in a directory. It prints one line for each
check, `ok` or `FAIL` first, and the hash of the files. The exit status is 1 when a check
fails. It asks no question, so a program can run it.

```text
$ clanker pack validate apps/paperboat
ok    manifest: paperboat 0.1.0 (Paperboat), schema 1
ok    deployment: layer 50-app, variable paperboat_version, chart chart
ok    releases: repository paperboat, first release 1.0.0
ok    services: paperboat-api, paperboat-web, paperboat-worker in the namespace paperboat
ok    debug commands: 4, each one a read
ok    checks: /api/healthz, /api/version, /api/inventory
ok    runbooks: db-backup, db-maintenance, restart-deployment, restore-drill
ok    eval cases: investigator_diagnosis
ok    hash: sha256 <64 hexadecimal characters>
```

`clanker pack import URL --tag TAG [--dir NAME] [--data DIR]` copies one version of a pack
from a git repository to this machine. A version is a tag. The command starts the `git`
program, so the operator runs it, not the core.

```text
$ clanker pack import https://git.example.com/harbor-pack.git --tag v0.1.0
pack     harbor 0.1.0 imported
source   https://git.example.com/harbor-pack.git tag v0.1.0 commit <commit>
sha256   <64 hexadecimal characters>
dir      <data directory>/packs/harbor/0.1.0
start    clanker up --pack <data directory>/packs/harbor/0.1.0
```

- The address is https, ssh, or a directory of this machine. An address with a password
  is refused: the address goes into the lock file and the audit log.
- `--dir` is the directory of the pack in the repository. The default is the root.
- A name that is a branch and not a tag is refused: a branch moves.
- The pack can hold plain files only. A symbolic link is refused. The limits are 5000
  files and 64 MiB.
- The lock file `<data directory>/packs/<name>/<version>.lock.json` has the address, the
  tag, the commit and the SHA-256 of the files.
- A second import of the same version with other files is refused. Give the pack a new
  version.

## Which pack the control plane reads

`clanker up` and `clanker setup` read the one pack in the directory `apps` of the source
files: the pack that comes with the control plane. `--pack DIR` names another one: a
directory of the source files, or a directory of this machine, for example the directory
that the import printed.

A pack with a lock file must still have the hash of the lock. A pack that changed on the
disk does not start; import it again. Each start with a new pack, a new version or other
files writes the event `pack.loaded` to the audit log, with the name, the version, the
source (`built in`, `directory`, or the address), the tag, the commit and the hash.

The core on a cloud has no disk. It reads only the pack that it carries.

## Not in the pack

- The stacks of the platform (`platform/stacks/*/50-app` and
  `platform/modules/shared/app`) name Paperboat and its variable. A pack for another
  application loads and passes the checks, but it cannot deploy until it brings its own
  application layer.
- A pack cannot change after the first start of an environment. The forge is seeded one
  time, so the runbooks and the chart of a new version do not reach it.
- The scenes of the simulator are the story of the Paperboat releases 1.1.0, 1.1.1 and
  1.2.0.
- The recorded model answers in `internal/agent/replay` are answers about Paperboat.
- The eval tasks other than `investigator_diagnosis` have their samples in the code of the
  suite, and those samples are about Paperboat.
