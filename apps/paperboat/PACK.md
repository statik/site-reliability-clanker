# The application pack of Paperboat

Paperboat is the example application of S.R.C. The control plane names no application in
its code. It reads what it must know about the application from an application pack: this
directory, with the manifest `pack.json`.

To manage another application, write a pack for it in `packs/<name>/`, and start the
control plane with `clanker up --pack packs/<name>`. The directory `apps/` holds exactly
one pack, Paperboat: a start with no `--pack`, the scripts in `scripts/verify/` and the
released binary read that one. CI runs `clanker pack validate` on each pack in `apps/` and
`packs/`, and `shellcheck` and `shfmt` on `packs/*/runbooks/*.sh`.

The package `internal/pack` reads and checks the manifest. `internal/pack/testdata/harbor`
is a second pack, which the tests use to show that the core does not depend on a name of
Paperboat.

## What the pack holds

| Path | Content |
|---|---|
| `pack.json` | The manifest |
| `chart/` | The deployment chart |
| `releases/` | One note `<version>.md` for each release |
| `build.sh` | Builds the images of every release and pushes them to a registry |
| `runbooks/` | The runbooks of the application: `<name>.json` and `<name>.sh` for each |
| `evals/` | The eval cases of the application: `<task>.json` for a task of the eval suite |

## The manifest

The format has the version 2. `docs/PACKS.md` has the design, with the parts that are not
built.

| Field | Meaning |
|---|---|
| `schema` | The version of the manifest format. This version reads `2` |
| `name` | The short name. The API uses it as the key of the application in its version lists. The platform names the namespaces of the pack after it, so it cannot be `default`, `kube`, `clanker`, `envoy-gateway-system`, `local-path-storage`, or start with `kube-` or `clanker-` |
| `title` | The name that a person reads. One to three words |
| `version` | The version of the pack, not of the application |
| `description` | One line about the application |
| `layer` | The platform layer that deploys the application |
| `version_variable` | The variable of the layer with the version to deploy. The layer has an output of the same name with the version that runs |
| `url_output` | The output of the layer with the address of the application |
| `releases.repository` | The repository of the forge with the release notes. Not a repository of the core |
| `releases.directory` | The directory of the notes, in the pack and in that repository. In the pack it can also hold a release record `<version>.json` beside each note (see "Release records") |
| `releases.first` | The release that a new environment starts with |
| `deploy.kind` | How the layer deploys the application. This version reads `helm` |
| `deploy.chart` | The directory of the main chart, inside the pack, with a `Chart.yaml`. Optional when every release has a record that names a chart |
| `deploy.values` | Optional. A directory with `values.yaml` and, for each cloud that needs other values, `values-<cloud>.yaml` (`local`, `aws`, `gcp`, `azure`). Files only, each 256 KiB or less |
| `deploy.extras` | Optional. The directory of a second chart, with a `Chart.yaml`, that the layer installs after the main chart: for objects that a chart of another project does not have |
| `deploy.build` | Optional. The build program, inside the pack. It gets the registry as its last argument. The installer on AWS gives `--platform linux/arm64` before it. A pack with none has its images in a registry already |
| `deploy.settings` | Optional. 1 to 16 variables of the layer that a change can set. Each has a `variable` (not the version variable and not a variable of the core), a `type` (`number`, a whole number from `min` to `max` within 0 to 1000000, or `bool` with no bounds), a `default` for a new environment, and the `value`: the dotted Helm path that it sets (names only, not below `platform`, and not inside the path of another setting) |
| `deploy.secrets` | Optional. 1 to 8 key names. The layer makes a secret with a random value for each key |
| `deploy.route` | The route of the application on the gateway of the platform: the `service` and the `port` that get the requests; optionally 1 to 8 path prefixes in `paths` (with none, every path), `timeout_seconds` from 0 (no timeout) to 3600 (with none, the default of the gateway), `hostname` (a host name of its own) and `wildcard` (every name below that host name; needs `hostname`) |
| `deploy.timeout_seconds` | Optional. The Helm timeout of an apply, 60 to 1200 seconds. The default is 300 |
| `deploy.workloads_namespace` | Optional. `true` asks the platform for a second namespace, for the pods that the application starts |
| `components` | The parts of the application that run. Each has a `name`, the `runtime` `kubernetes`, the `workload` (the name of its deployment) and `required`. No code reads `required` yet |
| `incident` | Optional. The `runbook` and the `component` that an incident proposes when no model gives a diagnosis. Without it, such an incident goes to a person |
| `diagnostics` | The reads of a debug session, 1 to 8. A read is `{"read": "logs", "component": …, "lines": …}` with 1 to 200 lines, or `{"read": "events"}` |
| `checks` | The HTTP checks, 1 to 8. Each has a `name`, the `kind` `http`, a `path` and optionally the `status` that passes (the default is 200; a value from 200 to 299). Exactly one has `"primary": true` |
| `app_version.source` | Where the monitor reads the version that runs: `http-json` or `image` |
| `app_version.path` | With `http-json` only. Answers with a JSON object that has the field `version` |
| `app_version.component` | With `image` only. A component of the pack. The version is the image tag of each of its instances, as the collector reports it |
| `topology.extra` | Optional. Nodes of the topology view that are not components |
| `topology.edges` | Edges between two nodes of the application, or one of them and a node of the platform |
| `runbooks` | Optional. The directory of the runbooks, inside the pack |
| `evals` | Optional. The directory of the eval cases, inside the pack |

**The checks.** The monitor repeats the primary check to measure the error rate. Its name
is in a finding: "fails 9 of 20 inventory requests". After an apply, each other check must
pass before the canary measures the primary check.

**The version.** The monitor removes one `v` at the start of a version and the build part
from the first `+`, and keeps a `-suffix`: `v2.38.11+abc1234` is `2.38.11`. A value that
is then not a version is unknown. With the source `image`, the check after an apply waits
for data from the collector that arrives after the apply. In that data each instance of
the component must be `ok` and run the wanted version. With no data from the collector
the check fails.

**The debug session.** The pack gives no command. The core starts with two commands of
its own, `kubectl get pods -o wide` and `kubectl get deployments`, and then makes one
command for each read: `kubectl logs deployment/<workload> --tail=<lines>` and
`kubectl get events --sort-by=.lastTimestamp`. It adds the namespace to each. The job
that goes to the runner has the reads and the namespace, not the commands
(`internal/jobspec/read.go`). The runner makes each command, and it starts no program for
a read that it does not know.

**Where the application runs.** The manifest has no namespace. The layer `40-platform`
has the output `targets` with the namespace of the components and the namespace of the
data services (`platform/CONTRACT.md`). The control plane reads the namespace of a debug
session and of the rights of its identities from that output. It refuses a namespace of
the cluster or of the control plane.

**Where the deploy files go.** The core puts every deploy file of a pack below
`apps/<name>/` of the platform repository, at names that the core chooses: the main chart
at `chart/`, the values at `values/`, the extras chart at `extras/`, the release records
at `releases/` and the chart archives that the records name at `charts/`. A pack cannot
choose a path there, and the forge refuses a seed in which two sources write one file. A
change below `apps/<name>/` touches the layer of the pack.

**The application layer.** One layer deploys every pack (`platform/CONTRACT.md`, `50-app`
and section 4). The control plane writes `app.tf` of each stack of the layer from the
manifest: the version variable, a variable with bounds for each setting, the URL and
version outputs, and the local value `app` with the deploy fields. It also writes the
lines of the pack in `terraform.tfvars` (the first release and the default of each
setting) and the line `app_name` of `40-platform`. The layer installs the objects of the
platform (the route, `<name>-platform`, `<name>-generated`), then the chart of the
release record or of `deploy.chart` with the values files, the values of the record, the
values of the platform and the settings, then the extras chart. With
`deploy.route.hostname` the application gets the host name `<name>.<app_domain>` of the
target, and its URL is on that name (`platform/CONTRACT.md` section 4). The layer does not
read `deploy.workloads_namespace` yet: step S11 of #148 adds it. `packs/nginx` is the
smallest pack that uses each of the other fields.

**Release records.** A pack can have a record `<version>.json` beside the note of each
release. When a pack has records, every release must have one, and each record must have
a note. The directory of the releases can hold only notes and records. A record has the fields below and no other:

| Field | Meaning |
|---|---|
| `chart`, `chart_sha256` | A chart archive of the pack, `charts/<file>.tgz`, 4 MiB or less, and its SHA-256 as 64 lowercase hexadecimal characters. The loader checks the SHA-256 |
| `chart_repository`, `chart_name`, `chart_version`, `chart_digest` | A chart that the runner fetches at apply: an `https://` or `oci://` address with no user, query or `..` step, the name of the chart, one exact version, and `sha256:<64 hexadecimal characters>` |
| `values` | Optional. An object of Helm values for this release, for example the image tag and digest |
| `schema_change` | Required. `true` when the release changes the schema of the database |

A record has a chart archive, a chart in a repository, or no chart; with no chart the
layer installs the chart of `deploy.chart`. How tightly each form pins the chart:

- **Archive in the pack.** The archive is in the reviewed pull request, and the loader
  refuses an archive whose SHA-256 differs from the record.
- **`oci://` repository.** `chart_digest` is the digest of the OCI manifest of the chart,
  the value that `helm pull` prints. It is not the SHA-256 of the archive. Helm 3.17 and
  later install a chart by that digest, `oci://<repository>/<name>:<version>@sha256:…`
  ([helm#12690](https://github.com/helm/helm/pull/12690)), and refuse a tag that
  resolves to another digest
  ([client.go](https://github.com/helm/helm/blob/v3.20.2/pkg/registry/client.go#L925-L943)).
  The Helm provider 3.3.0 of the stacks contains Helm 3.20.2 and gives the chart
  reference to Helm as it is
  ([resource_helm_release.go](https://github.com/hashicorp/terraform-provider-helm/blob/v3.3.0/helm/resource_helm_release.go#L1307-L1335)),
  so the layer gives the chart as `oci://<repository>/<name>@<digest>` with
  `version = <version>`. With the provider 3.3.0 a plan fetched a chart in that form, an
  apply on kind installed it and a second plan had no changes; a digest of another version
  stopped the plan with `chart reference digest mismatch`.
  A digest in the `version` argument does not work. Coder
  publishes its chart at `oci://ghcr.io/coder/chart/coder` as well as at
  `https://helm.coder.com/v2`
  ([Coder docs](https://github.com/coder/coder/blob/19da85b5d353982565298e964f657b1ea6a25567/docs/install/server/kubernetes/index.md#L133-L143)).
- **`https://` repository.** `chart_digest` is the `digest` of the version in the
  `index.yaml` of the repository (the SHA-256 of the archive), with `sha256:` before it.
  Helm does not compare a downloaded archive with that digest
  ([chart_downloader.go](https://github.com/helm/helm/blob/v3.22.1/pkg/downloader/chart_downloader.go#L88-L101)),
  and the Helm provider has no argument for it (its `verify` checks a GPG provenance
  file, which Coder does not publish). So the version pins the chart, and the digest only
  records the archive that a person reviewed.

The release detector reads only the notes, `<version>.md`, in the releases repository. For
a pack with records, a release whose record is not in the platform repository at
`apps/<name>/releases/<version>.json` gets a finding for a person: "<title> <version> is
published; add its record … in a pull request". The agent does not draft a change for
it. When the record is on main, the next check opens the upgrade for the agent.


## A pack is not trusted

A pack comes from outside the core. `pack.Load` refuses a manifest when:

- it has a field that the format does not have, also inside `deploy`, a component, a read
  or a check;
- a name, a path or a version does not match its fixed pattern;
- a path can leave the pack;
- `deploy.kind`, the runtime of a component, the kind of a check or the source of the
  version is one that this version does not read;
- a component or its workload comes twice, or a component has the name of a node of the
  platform;
- a read is not `logs` or `events`, a read of logs names a component that the pack does
  not have or asks for more than 200 lines;
- the path of a check is not a plain path, the status that passes is not from 200 to 299,
  or the number of primary checks is not one;
- `incident` names a component that the pack does not have, a runbook that is not a
  mitigation for an incident in the pack, or a component that this runbook does not
  permit;
- an edge of the topology joins nodes that do not exist, or two nodes of the platform;
- the name is a reserved name (see `name` above);
- a deploy field is outside its bounds (see the table above), a setting has the name of
  the version variable, of a variable of the core or of another setting, or its Helm path
  has an index, an escape, a second value, a value of the platform or the path of another
  setting;
- the chart, the values, the extras chart, the build program or the note of the first
  release is not in the pack, a chart has no `Chart.yaml`, or the directory of the values
  has a file that is not `values.yaml` or `values-<cloud>.yaml`. The loader does not parse
  the YAML of a values file;
- the directory of the releases has a file that is not a note or a record, a release has
  no record when others have one, a record has no note, a record has a field that the
  format does not have, has no `schema_change`, names an archive that is not in the pack
  or has another SHA-256, or names a repository that is not `https://` or `oci://`, a
  version that is not exact, or no digest;
- `deploy.chart` is missing and a release has no record that names a chart;
- the directory of the runbooks or of the eval cases has a directory in it, a file of
  more than 256 KiB, a script with no definition, a definition with no script or a
  definition that `internal/runbook` refuses;
- a runbook that the Investigator can propose has no `title`, has a `title` that is not
  one to five words, does not have the input `component`, has a second input, or permits
  a value that is not the name of a component. The name of a workload is not the name of
  a component;
- an eval case file does not have `schema` 2, the task of its name, and 1 to 200 samples
  that each have their own `id`.

Only names that passed these checks go into the prompt of a model, through four
placeholders:

| Placeholder | Text |
|---|---|
| `{{releases_repository}}` | The repository with the release notes: `paperboat` |
| `{{releases_directory}}` | The directory of the notes in that repository: `releases` |
| `{{components}}` | The components, each with its deployment: `api (the deployment paperboat-api), web (the deployment paperboat-web) or worker (the deployment paperboat-worker)` |
| `{{mitigations}}` | The runbooks that are mitigations for an incident, in the order of their names: `restart-component`. The word `none` when the pack has none |

## Runbooks

The runbooks of an application are in the pack, because they name its components and its
database. A runbook that is a mitigation for an incident gets the name of a component, and
its script makes the name of the deployment. At the first start, the control plane puts them in the directory `jobs/` of the
runbooks repository, beside the policies and the notification routes of the core. A pack
can put files only there: it cannot bring a policy, change a risk tier or add a route.
`runbooks/README.md` has the format of a definition.

A script names no namespace. The runner gives it the targets of the environment as
`TARGET_APPLICATION` and `TARGET_DATA`; `runbooks/README.md` has the full environment.
`TARGET_DATA` is empty on AWS, GCP and Azure, where the database is not in the cluster,
so the database runbooks of Paperboat (`db-backup`, `db-maintenance`, `restore-drill`)
stop there with a message before they run a command.

## Eval cases

`evals/investigator_diagnosis.json` has the incidents that the eval suite gives to the
Investigator role: for each one the transcript of the debug session, the true cause, the
mitigation that is acceptable, and the evidence that the answer must cite. The field
`expect` of a case is `none`, `<runbook>:<component>`, or `either:<runbook>:<component>`
when that mitigation and no mitigation are both acceptable. The suite
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
ok    manifest: paperboat 0.1.0 (Paperboat), schema 2
ok    deployment: layer 50-app, variable paperboat_version, chart chart, Helm timeout 300 s, into apps/paperboat/ of the platform repository
ok    application layer: 50-app/app.tf with the variable paperboat_version, 4 settings and the outputs app_url and paperboat_version
ok    settings: api_replicas 1 to 5 (api.replicas, default 1), worker_replicas 1 to 5 (worker.replicas, default 1), web_replicas 1 to 5 (web.replicas, default 1), loadgen_enabled bool (loadgen.enabled, default true)
ok    route: service paperboat-web port 8080, every path, the request timeout of the gateway
ok    secrets: none
ok    releases: repository paperboat, first release 1.0.0
ok    release records: none
ok    components: api (paperboat-api), web (paperboat-web), worker (paperboat-worker)
ok    diagnostics: 2 reads, and the state of the pods and the deployments
ok    checks: ready /api/healthz 200, inventory /api/inventory 200 (primary)
ok    version: http-json /api/version
ok    incident: restart-component on api without a diagnosis of a model
ok    runbooks: db-backup, db-maintenance, restart-component, restore-drill
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
directory of the source files such as `packs/<name>`, or a directory of this machine, for
example the directory that the import printed.

A pack with a lock file must still have the hash of the lock. A pack that changed on the
disk does not start; import it again. Each start with a new pack, a new version or other
files writes the event `pack.loaded` to the audit log, with the name, the version, the
source (`built in`, `directory`, or the address), the tag, the commit and the hash.

The core on a cloud has no disk. It reads only the pack that it carries.

## Not in the pack

- The released binary and the core on a cloud carry Paperboat only.
- A pack cannot change after the first start of an environment. The forge is seeded one
  time, so the runbooks and the chart of a new version do not reach it.
- The scenes of the simulator are the story of the Paperboat releases 1.1.0, 1.1.1 and
  1.2.0.
- The recorded model answers in `internal/agent/replay` are answers about Paperboat. Each
  file name starts with `paperboat_`, the name of the pack, and the replay mode reads only
  the files of the pack that runs. Another pack gets the template text of the Explainer and
  of the Investigator, no review, and no pull request from the Author for a release or an
  advisory.
- The eval tasks other than `investigator_diagnosis` have their samples in the code of the
  suite, and those samples are about Paperboat.
