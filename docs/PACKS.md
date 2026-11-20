# Application packs — design

This document has two parts. Part A says how an application pack works today and what the
core requires from it. Part B is a proposal: a standard way to gather health and
observability data, so that a pack can describe a legacy application that does not run on
Kubernetes, also on servers in a data centre. Nothing in part B is built. Section B.13
has what is decided and what is open.

`docs/PLAN.md` section 5.7 has the rules of a pack. `apps/paperboat/PACK.md` has the fields
of the manifest and the commands.

## Part A — How a pack works before schema 2

Part A describes the code at the commit `d182274`, before the first step of section B.14.
The places in the code are those of that commit. `apps/paperboat/PACK.md` has the manifest
of today.

### A.1 The path of a pack

1. **Import.** `clanker pack import URL --tag TAG` clones one tag, checks the pack and
   copies it to `<data>/packs/<name>/<version>/`. A lock file keeps the address, the tag,
   the commit and the SHA-256 of the files (`internal/packstore/packstore.go`).
2. **Load.** `clanker up` and `clanker setup` read one pack: the one in `apps/`, or the one
   that `--pack` names. `pack.Load` refuses an unknown field, checks each value against a
   fixed pattern and checks that the files exist (`internal/pack/pack.go:124`).
3. **Seed.** At the first start of an environment, the control plane copies the chart to
   `<mount>/<chart>` in the platform repository, the note of the first release and the
   chart to the releases repository, and the runbooks to `jobs/` in the runbooks
   repository (`cmd/clanker/up.go:175`). A pack cannot change after that
   (`apps/paperboat/PACK.md`, "Not in the pack").
4. **Use.** One `pack.Pack` value goes to each component of the core. The table shows what
   each one reads.

| Component | Reads from the pack | For |
|---|---|---|
| Detector (`internal/detect`) | `layer`, `version_variable`, `releases.*`, `title`, `health.probe_name` | The release finding and the incident finding |
| Monitor (`internal/monitor`) | `layer`, `url_output`, `version_variable`, `health.*` | Health samples, the check after an apply, snapshots |
| Orchestrator (`internal/orchestrator`) | `chart`, `mount`, `layer`, `namespace`, `debug_commands` | The layers that a change touches, the debug session |
| Agent (`internal/agent`) | `deployments`, `incident_deployment`, `releases.*`, `namespace`, `health.probe_name` | Prompts, the mitigation, the write-up |
| API (`internal/api`) | `name`, `title`, `layer`, `version_variable`, `url_output`, `releases.*`, `topology.*` | Version lists, the topology view |
| Local start (`cmd/clanker/up.go`, `internal/localtarget`) | `namespace`, `data_namespace`, `build` | The rights of the identities, the images |
| Simulator (`internal/sim`) | Names of the pack, and facts of Paperboat that no field has | The scenes of the demonstration |
| Eval suite (`evals/`) | `evals/` | The samples of `investigator_diagnosis` |

### A.2 The required interface

`apps/paperboat/PACK.md` lists the fields. The core also depends on things that no field
says. A pack that does not meet them loads, but it does not work.

**The layer of the application**

- `layer` must be `50-app`. The pattern permits each name of the form `NN-name`, but the
  layers are a fixed list (`internal/orchestrator/types.go:56`), the drift check plans a
  literal `50-app` (`internal/detect/detect.go:255`), and the identities of the local
  target name it (`internal/localtarget/identities-app.yaml`).
- `mount` must be `apps/paperboat`: the stack has the path of the chart as a literal
  (`platform/stacks/local/50-app/main.tf:34`).
- The layer has an output with the name of `url_output`, one base URL
  (`internal/monitor/monitor.go:38`), and an output with the name of `version_variable`,
  the version that runs (`internal/monitor/monitor.go:172`).
- When the URL output is absent, nothing fails. The check after an apply passes
  (`internal/monitor/monitor.go:173`) and the monitor skips the environment
  (`internal/monitor/monitor.go:129`).
- An upgrade is a change of one line `name = value` in the file `terraform.tfvars` of the
  layer (`internal/agent/author.go:91`). Versions are three numbers; a suffix does not
  change the order (`internal/detect/detect.go:129`).
- The pack must have a chart, a build program and the note of the first release
  (`internal/pack/pack.go:146`). There is no other form of deployment code.

**Health**

- Health is three HTTP `GET` requests on that one URL. No other protocol exists. Only the
  status 200 passes, and a request has 4 seconds (`internal/monitor/monitor.go:34`, `:97`).
- `health.version_path` answers JSON with a string field `version`
  (`internal/monitor/monitor.go:86`).
- A sample is `ok`, `total`, `p95_ms` and `version` for one probe path, with the time and
  the environment (`internal/monitor/monitor.go:110`).
- The limits are in the code of the core, not in the pack:

| Rule | Value | Place |
|---|---|---|
| The check after an apply waits for ready and the wanted version | 90 s | `internal/monitor/monitor.go:178` |
| The canary fails | 10 % errors or more, of 40 requests | `internal/monitor/monitor.go:208` |
| An incident opens | More than 20 % errors in each of the last 3 samples, when no change applies and no incident is open | `internal/detect/detect.go:303` |
| An incident is recovered | 20 of 20 requests succeed, inside 90 s | `internal/agent/incident.go:104` |
| The topology view shows a node as unhealthy | Under 90 % success, and only on the node with the name `api` | `internal/api/views.go:408`, `:416` |

**The debug session**

- Each debug command is a `kubectl` argument list. The verbs are `get`, `logs`, `describe`
  and `top` (`internal/pack/debug.go:11`). The core adds `-n <namespace>`
  (`internal/pack/pack.go:200`).
- The job type carries a kubeconfig and `kubectl` argument lists
  (`internal/jobspec/jobspec.go:71`, `:81`), and the runner starts `kubectl`
  (`internal/runner/client.go:203`, `internal/runner/runner.go:559`).
- On the local target the credential is a token for the identity `debugger`, which has the
  role `view` in the namespace of the pack only (`internal/localtarget/identities-app.yaml`).
- The transcript has a line `$ kubectl …` before the output of each command
  (`internal/orchestrator/runbook.go:369`). The output is scrubbed and cut to its last
  2800 characters (`:315`). The model gets 14000 characters at most
  (`internal/agent/incident.go:37`).

**Mitigation and runbooks**

- The Investigator gives a mitigation runbook one parameter, `deployment`
  (`internal/agent/incident.go:84`). `pack.Load` checks the values of that input only when
  the input is there (`internal/pack/parts.go:157`). A mitigation runbook without it
  loads, and then each incident goes to a person.
- A runbook script gets `KUBECONFIG` as its credential (`internal/runner/runner.go:485`).
  It also gets `STATE_DIR`. On the local target that directory holds the kubeconfig of
  the administrator (`cmd/clanker/up.go:300`), so a script of a pack can read it there.
- The rights of the identity `runbook` are in the core: on the local target it can patch
  deployments and read pods in `namespace`, and start a command in a pod of
  `data_namespace`. It has no right on a stateful set. When the namespace does not exist,
  the rights are not bound and nothing reports it
  (`internal/localtarget/credentials.go:69`).
- The prompt of the Investigator names the runbook `restart-deployment` and speaks of pods
  that are "Running and Ready" (`internal/agent/prompts/investigator.txt:7`, `:12`). The
  answer without a model names `restart-deployment` (`internal/agent/incident.go:41`), and
  the write-up says "Restart" (`internal/agent/incident.go:183`, `:216`).
- The eval cases expect `restart:<deployment>` and hold `kubectl` transcripts
  (`apps/paperboat/evals/investigator_diagnosis.json`). The checks of the suite name
  `restart-deployment` too (`evals/src/clanker_evals/checks.py:20`), so they are not the
  same for every pack.

**Topology, storage and the simulator**

- The nodes of the platform are a fixed list with `cluster` and `nodes` in it
  (`internal/pack/pack.go:114`).
- The storage check reads samples of one volume with a fixed name, `postgres-data`, and
  proposes a larger value of one variable of the layer `30-data`
  (`internal/monitor/volume.go:8`, `internal/detect/storage.go:175`). Only the simulator
  writes these samples (`internal/sim/storage.go:63`). No collector reads the fill level
  of a real volume.
- The simulator needs the runbooks `db-backup`, `db-maintenance` and `restore-drill` and a
  fault endpoint of Paperboat (`internal/sim/simulator.go:204`, `:302`).

### A.3 What stops an application that is not on Kubernetes

| Need | Today |
|---|---|
| Say what the parts of the application are | `deployments`, `namespace`, `data_namespace` |
| Read the state of a part | `kubectl get` in a namespace |
| Read logs and events | `kubectl logs`, `kubectl get events` |
| Check health with no HTTP endpoint | Not possible |
| Check health when the core has no network path to the application | Not possible: each request counts as an error |
| Find the version with no version endpoint | Not possible |
| Limit a credential to the application | A Kubernetes role in a namespace |
| Give a runbook its credential | `KUBECONFIG` |
| Ship the deployment code | A chart |
| Start a runner near the application | Only a process on the machine of the core (`internal/launch/local.go:31`) |

## Part B — Proposal: one standard for health and observability

### B.1 Goals

1. One pack format describes an application on Kubernetes, on virtual machines, and on
   servers that exist already in a data centre.
2. The detectors, the Investigator and the eval suite read one format of data. They do not
   read the output of a tool of one runtime.
3. The rules of `docs/PLAN.md` section 5.7 stay true. A pack is untrusted input. A pack
   cannot give a model a tool, and no text of a pack becomes a command. A pack does not
   install the guard that limits it.

Not a goal: a general monitoring system. The core keeps only what its detectors and the
Investigator use, and a fixed list of series for the charts of section B.16.

### B.2 Terms

| Term | Meaning |
|---|---|
| Component | One part of the application that runs: a deployment, a service of the operating system. It replaces "deployment" in the manifest, the runbooks, the prompts and the eval cases |
| Instance | One copy of a component: a pod, or the unit on one host |
| Runtime | The kind of system that runs a component. The core has a closed list of runtimes |
| Collector | The OpenTelemetry Collector: a program that stays on a host, in a cluster or in a site, gathers data and sends it to the core |
| Check | A test of the application: a request, a connection, the result of a job |
| Observation | The record that the core makes from the data of the collectors and of the checks. It has one format for every runtime |
| Site | A network that the core cannot reach from outside: a data centre, a private network |
| Site runner | A runner that stays inside a site and gets its jobs from the core |

### B.3 The pack declares, the collector collects

**Decided 2026-11-20 by the owner:** the OpenTelemetry Collector gathers the standing
health and observability data. The project does not write its own collectors for that
data. The read of logs in a debug session stays apart, under a grant (section B.7); that
part is the recommendation of this document, and section B.13 has it as an open row.

A pack does not bring a configuration of the collector, and it does not bring a program.

| | A. Declared (this design) | B. Scripts or configuration in the pack |
|---|---|---|
| The pack brings | Names and numbers: the components, their runtime, the checks | Programs that read the application, or a configuration of the collector |
| What runs | The collector of the project, with a configuration that the core writes | The code or the configuration of the pack |
| A new kind of application needs | A new runtime in the core, when no runtime fits | Only a new pack |
| What a pack can make a host do | Nothing outside the fixed reads | Any read that the collector can do, and any use of the network |

The core writes the whole configuration of a collector from a template. A value of a pack
fills only a place for one checked value: a unit name, a port, a path below a permitted
directory, a pattern. Section B.7 has the rules.

**What the collector gives, and what the project still writes.** The versions are those of
2026-11-20: the collector core 0.165.0 and the contrib components 0.165.1. The stability
is what each component states.

| Data | Component of the collector | Stability |
|---|---|---|
| State and instances on Kubernetes | Receiver `k8s_cluster` | Beta |
| Volume fill and pod memory on Kubernetes | Receiver `kubeletstats` | Beta |
| Image tag, start time of a pod | Receiver `k8s_cluster`, processor `k8sattributes` | Beta, stable |
| Active state and restarts of a unit | Receiver `systemd` | Alpha |
| Fill of a file system, memory | Receiver `hostmetrics` | Beta |
| HTTP and TCP checks | Receivers `httpcheck`, `tcpcheck` | Alpha |
| Lines of a log file, lines of the journal | Receivers `filelog`, `journald` | Beta, alpha |
| A count of log lines that match a pattern | Connector `count` | Alpha |
| Sending to the core, with a queue on the disk | Exporter `otlphttp`, extension `file_storage` | Stable, beta |

The project writes:

1. **The generator of the configuration**, from the manifest and the approved targets.
2. **The endpoint of the core** that receives the data and reduces it to the observation
   (section B.6).
3. **One receiver of its own**, built into the collector, for what no component gives on a
   host: the sub state and the result of a unit, the start time of its main process, the
   last run of a timer and its result, and the first line of a version file. The rule for
   a service that fails in a loop and the check kind `job` need it.
4. **The reads of a debug session**, on a host and on Kubernetes (section B.7).
5. **A build of the collector** with only these components, made with the builder of the
   collector project, at a pinned version. The full contrib archive has 115 MB; the
   smallest official build has 13 MB. The size of this build was not measured.

Five of the components are alpha. Their names of metrics and their settings can change
between versions, so the version is pinned and a new version is a reviewed change.

### B.4 Components and runtimes

The manifest gets a list `components`. It replaces `deployments`, `namespace` and
`incident_deployment`.

| Field | Meaning |
|---|---|
| `name` | The name of the component in findings, prompts, runbook inputs and the topology |
| `runtime` | `kubernetes` or `systemd`. Section B.13 has a third, `docker` |
| `workload` | For `kubernetes`: the name of the deployment or the stateful set |
| `unit` | For `systemd`: the name of the unit, for example `ledger-app.service` |
| `required` | `true` when the application does not work without this component |
| `shared` | Optional. `true` when the unit serves other applications too, for example a web server with several sites. No mitigation runbook can name a shared component |
| `log_path` | Optional, for `systemd`: a log file of the component, when it does not write to the journal. The logs then come from this file, which also keeps the logs of a shared unit apart |

**Where a component runs.** The pack does not say. A layer of the platform does, with one
new output, `targets`: for each component, the namespace on Kubernetes, or the list of
host addresses for `systemd`.

- The output is in a layer below the layer of the application: the layer `40-platform`,
  which has the namespace today, or the layer of the site. The pack brings neither. The
  identity that applies the layer of the application then has its rights before the
  first apply of that layer.
- The value comes from the state that an approved apply wrote, the same as `url_output`.
- For hosts that no layer creates, the layer of the site takes the addresses as
  variables and returns them as outputs. A person approves that apply, so a person
  approves the inventory.
- The approval page shows the targets. The core refuses a namespace that belongs to the
  cluster or to the control plane, and an address that is loopback, link-local or
  outside the ranges that the environment permits.
- The broker binds the rights of the identities to the targets after that apply. Today it
  binds them from the manifest (`internal/localtarget/credentials.go:59`).
- On a host, the units and the log files of the manifest are a part of the scope, and
  the pack writes them. Section B.7 says who approves them.

**What is gathered.**

| Data | `kubernetes` | `systemd` |
|---|---|---|
| State | Wanted and ready instances of the workload, the phase of each pod | The active state of the unit on each host; the sub state and the result from the receiver of the project |
| Restarts | The restart count of each container | The count of automatic restarts of the unit |
| Start time | The start time of the pod | The start time of the main process, from the receiver of the project |
| Version | The image tag of each instance | The first line of the version file, from the receiver of the project |
| Volumes | Capacity and free space of each volume claim | The use of the file system of each path |
| Memory | The memory of each pod | The memory of the unit |
| Logs and events | Not gathered by the collector | Not gathered by the collector |

- The collector sends no log line and no event to the core. Section B.7 says why and how
  the Investigator reads them.
- The receiver of the project reads named properties of a unit only. The full properties
  of a unit can hold its environment variables, and those can hold a secret.
- The active state alone is not enough. A service that fails and starts again in a loop
  reports "activating", so the sub state is needed.
- On Kubernetes, the fill of a volume comes from the statistics of a node. No role in a
  namespace can read them: the collector needs the right `get` on `nodes/stats` for the
  cluster. This collector therefore belongs to the layer of the platform, not to a pack.
  The path through the proxy of the API server needs the right `nodes/proxy`, which
  Kubernetes documents as more than a read. The design does not use it.

**The states.** The core maps what the collector sends to four states.

| State | `kubernetes` | `systemd` service | `systemd` timer |
|---|---|---|---|
| `ok` | Each wanted instance is ready and has the new specification | Active and running on each host, with no new restart | The timer is active: it waits for its next run |
| `degraded` | One instance or more is ready, but not all; or a container started again since the last observation | Active on some hosts; or it starts, stops or reloads; or it started again since the last observation | Active on some hosts |
| `down` | Instances are wanted and none is ready | Failed, inactive, or in a loop of restarts, on each host | The timer is not active |
| `unknown` | No data arrived in time, or the cluster did not yet read the last change | No data arrived in time from a host, or the unit is not there | No data arrived in time |

- `unknown` is a real value. The detectors do not read `unknown` as `ok`.
- The four states are those of a long convention of monitoring programs: OK, warning,
  critical, unknown.
- A timer and the service that it starts are two units, and both are read. The state of
  a timer says only that it will run. The result of its last run is the check of the kind
  `job` (section B.5), so a timer that had no run since an apply is `ok`.
- A service that runs one time and ends is healthy when it is inactive and its result is
  a success.

**Volumes.** The manifest gets a list `volumes`: a `name`, a `component`, and `claim` (the
volume claim, for `kubernetes`) or `path` (a file system path, for `systemd`). An optional
`size_variable` names the variable of the layer that sets the size. On a host, the
collector reports a file system by the point where it is mounted, so the host kit
(section B.7) finds that point for each path when it writes the configuration.

**Diagnostics.** The debug session of a pack becomes a list of reads. It replaces
`debug_commands`:

```json
"diagnostics": [
  {"read": "logs", "component": "api", "lines": 30},
  {"read": "events"}
]
```

A read is `logs` or `events`, optionally with one component of the pack, and `lines` from
1 to 200. There is nothing else to check: no flag, no word, no format. The state, the
volumes and the checks are not reads: the Investigator gets the newest observation.

### B.5 Checks

`health` becomes a list `checks`. Each check has a `name` with the pattern of a component
name, a `kind`, and the fields of its kind.

| Kind | Fields | Passes when |
|---|---|---|
| `http` | `path`, optional `status` (default 200), optional `from`; with `from` `host` also `component` and `port` | The answer has the status |
| `tcp` | `address_output`; or, with `from` `host`, `component` and `port` | The connection opens |
| `log-match` | `component`, `pattern`, `limit` (0 to 100) | The log lines of the component in the last 5 minutes have `limit` lines or fewer that match the pattern |
| `job` | `component` (a timer), `max_age_hours` (1 to 168) | The last run passed and is not older than the limit |

**Where a check runs.** `from` has two values.

| `from` | The check runs | Against |
|---|---|---|
| `outside` (default) | In the core, with the code of today, when the core has a network path. For an environment in a site: in the collector of the site, which runs beside the site runner | The URL of `url_output`, or each address of `address_output`: an output of the layer with a list of `host:port` values, under the address rules of section B.4 |
| `host` | In the collector on each host of `component` | `localhost` and `port`. For a database that listens only on its host, or a port behind a load balancer |

- The check of the core sends many requests in one sample, as today. A check of the
  collector gives one result for each interval. Section B.8 has the rules for both.
- A check with no result in time has the state `unknown`. It is not a failed check. Today
  a request with no network path counts as an error (`internal/monitor/monitor.go:93`).

**The primary check.** One check has `"primary": true`. The incident rule and the recovery
rule read it. It is of the kind `http`, `tcp` or `job`. An application with no listener,
for example a set of jobs that run at night, has a `job` check as its primary check.

**The other checks.** Each check must pass in the canary after an apply. A check that is
not primary opens no incident when it fails. Its result is in the observation, for the
Investigator and for the dashboard.

**Limits.** `status`, `limit` and `max_age_hours` say what one check is, as the probe path
does today. They do not change the rules of section B.8, which the core fixes.
`clanker pack validate` prints them and the event `pack.loaded` records them.

**`log-match`.**

- The pattern goes into the configuration of the collector on the host. The collector
  counts the lines that match and sends only the count. No log line leaves the host for
  this check.
- The pattern is a value in a configuration file. It is never a part of a command line.
- The pattern is a regular expression of the Go syntax, 200 characters at most. The Go
  package guarantees a run time that is linear in the size of the input.
- The collector reads `${…}` in a value as a reference to a file or to a variable of the
  environment. The generator writes each `$` of a pattern in the escaped form, and the
  build of the collector has no source of configuration over the network.
- The count of the collector grows without end. The core takes the difference between
  two observations.

**The version that runs** comes from `app_version`, with one of these sources. The field
`version` of the manifest is the version of the pack.

| Source | Fields | The version is |
|---|---|---|
| `http-json` | `path` | The field `version` of the JSON answer. This is the source of today. It gives one value for the application, in the field `version.running` of the observation. Only the core can read it, so it is not for an environment in a site |
| `file` | `component`, `path` | The first line of a file on each host of the component. The path is relative to a directory that the host permits (section B.7). A symbolic link that leaves that directory is refused |
| `image` | `component` | The tag of the image of each instance. For `kubernetes`, and for `docker` |
| `layer` | none | The output of the layer. The control plane then knows only what it deployed, not what runs |

- A value that does not match the pattern of a version is `unknown`.
- Instances of one component that report different versions are a failed check.
- With the source `layer`, no running version is checked. The approval page of the first
  deployment and the text of each canary say so, and `clanker pack validate` prints it.
  Section B.13 has the alternative: refuse the source.

### B.6 The observation

The core reduces the data of the collector and of its own checks to one record. This is
the standard that the detectors, the Investigator and the eval cases read.

```json
{
  "schema": 1,
  "env": "prod",
  "at": "2026-11-20T10:15:00Z",
  "application": "ledger",
  "version": {"wanted": "4.2.1", "source": "file"},
  "components": [
    {"name": "app", "runtime": "systemd", "state": "ok",
     "instances": [
       {"id": "host-1", "state": "ok", "version": "4.2.1", "restarts": 0,
        "started": "2026-11-11T02:10:00Z"},
       {"id": "host-2", "state": "ok", "version": "4.2.1", "restarts": 0,
        "started": "2026-11-11T02:14:00Z"}
     ]}
  ],
  "checks": [
    {"name": "login", "kind": "http", "primary": true, "ran_from": "site",
     "state": "ok", "ok": 20, "total": 20, "p95_ms": 41}
  ],
  "volumes": [
    {"name": "app-data", "instance": "host-1", "used": 41200000000, "total": 107000000000}
  ],
  "logs": [{"component": "app", "instance": "host-1", "text": "…"}],
  "events": [{"at": "2026-11-20T10:02:11Z", "component": "app", "text": "…"}]
}
```

- `state` is one of `ok`, `degraded`, `down` and `unknown`, for a component, an instance
  and a check.
- `ran_from` is `core`, `site` or `host`.
- With the version source `http-json`, `version` also has the field `running`, and the
  instances have no version.
- `logs` and `events` are only in the observation of a debug session. Their `text` holds
  text of the application. The core scrubs it and frames it as untrusted data, as it
  does with the transcript today.
- No other field holds free text. A version must match the pattern of a version.
- `restarts` is the number of restarts since the last observation. The collector sends a
  count that grows and that can go back to zero; the core takes the difference.
- `started` is the time at which the main process of the instance started last.
- `ok`, `total` and `p95_ms` of a check of the collector come from its results in one
  sample period (section B.8).
- An entry for memory has the form of a volume entry, in its own list. `total` is absent
  when the component has no limit.

The core keeps the record and the series of section B.16, and nothing else. It is not a
general store for metrics: what a collector sends and neither of them uses is dropped.

Who reads it:

| Reader | Today | With the observation |
|---|---|---|
| Health history | `ok`, `total`, `p95_ms`, `version` of one probe | One row for each check and each component state, by name |
| Incident detector | Error rate of the probe | Section B.8 |
| Storage detector | Samples that only the simulator writes | `volumes`. It proposes a larger size only for a volume with a `size_variable`. For a disk of a server it opens the finding for a person and proposes no change |
| Investigator | The text of `kubectl` commands | The newest observation, and the logs and the events of the debug session as scrubbed text |
| Topology view | A health value on the node `api` | The state of each component |
| Eval cases | `kubectl` transcripts | Observations. `expect` becomes `<runbook>:<component>`. The format of an eval case file gets the version 2, and the checks of the suite name no runbook |

The Investigator prompt then speaks of components in the state `ok`, not of pods that are
"Running and Ready", and it reads the list of runbooks that are mitigations from the
definitions, not from a name in its text. The prompts change, so the evals of every role
that changes run again before the change merges.

### B.7 Where collection runs, and with which identity

| Data | Gathered by | Identity |
|---|---|---|
| Checks `from` `outside` and the `http-json` version, when the core has a path | The core, as today | None: a network request |
| State, restarts, versions, volumes, memory, the other checks, the count of `log-match`, the `job` check | A collector that stays: one on each host, one in each cluster, one in each site for the checks from outside | A local user on a host, or a service account in a cluster, with read rights only. This is the identity `observer` of `docs/PLAN.md` section 6.1: standing and read-only |
| Logs and events for the Investigator | A runner, as the job kind `diagnose` | `debugger`: no standing access, a grant of 15 minutes, the session recorded |

**Why logs do not go through the collector.** A collector sends all the time. It has no
read on request, and nothing to bind a grant to. To send the logs through it, the core
must become a store for the logs of the application, and the rule "the debugger has no
standing access" of `docs/PLAN.md` section 6.1 must end. This design keeps the rule: the
collector sends numbers and states, and a log line leaves a host only in a recorded debug
session. Section B.13 has the alternative.

**From the collector to the core.**

- A collector sends over HTTPS, outbound only, in the OTLP format. This fits a site.
- In a site, a collector on a host sends to the collector of the site, and that one
  sends to the core. A host then needs no path out of the site. The receiver of the
  collector for this format is stable.
- The core receives the data in its own process, with the data library of the collector
  project. It starts no program.
- Each environment has its own token: one for a site, one for a cluster, one for the
  hosts of an environment outside a site. The token is a standing secret that the kit or
  the layer of the platform installs. It is not a credential of the broker, and it gives
  only the right to send.
- The core accepts only data for the hosts, the units, the namespaces and the workloads
  of the approved targets and manifest of that environment, and drops the rest. A
  request has a size limit and an environment has a rate limit.
- The core stamps an observation with its own clock, which the simulator can move. It
  uses the time in the data only to find the age of the data. A collector keeps a queue
  on its disk while the core is not reachable and sends it later. Data that is older
  than a few intervals goes to the history and opens no incident.
- Silence is a state. When no data arrives from a collector for 3 intervals, its
  components and checks are `unknown`.

**The configuration of a collector.**

- The core writes it from a template, the manifest and the approved targets. One value
  comes from the host: the point where the file system of a volume path is mounted. The
  kit finds it and the core checks it against the permitted directory.
- A value of a pack fills only one checked place. No pack value reaches these settings:
  the files or the units that a receiver reads other than those of the manifest, the
  address or the headers of a check, the address that the collector sends to, the
  program of the journal receiver.
- The statements of the transform language of the collector are fixed in the template.
  The pattern of a `log-match` check fills one string in one fixed statement, escaped for
  that string and for `$` (section B.5). The statement reads the message of a log line,
  not the full entry of the journal, which also has the command line of the process.
- The journal receiver starts the program `journalctl` with the unit names as arguments.
  The pattern of a unit name permits no `-` at its start.
- The configuration is a file. It goes to the host or to the cluster with the first
  deployment, a tier T3 change, so a person approves what the collector reads. The remote
  management protocol of the collector project (OpAMP) is not used: its parts for the
  collector are alpha.

**On Kubernetes.** The layer of the platform installs the collectors: one for the cluster,
which reads the state with a role in the namespace of the application, and one on each
node, which reads the statistics of that node. The receiver for those statistics reads
one node only. Its right is for the cluster (section B.4).

**The site runner.** A site still needs a runner for jobs: a plan, an apply, a runbook, a
debug session. It needs a network path to the hosts and outbound HTTPS to the core. The
direction of the protocol fits already: each call goes from the runner to the core with a
token (`internal/runner/client.go:35`). What is missing:

- A runner that stays. Today the core starts a runner for each job, and only as a process
  on its own machine (`internal/launch/local.go:31`). A site runner is enrolled one time,
  has an identity for its site, and asks the core for the jobs of its environments. This
  changes the choice "Core and runners" of `docs/PLAN.md` section 12, which says that a
  runner is short-lived and that the core starts it.
- A time limit for a job that no runner takes.

A site with no outbound path to the core needs the core inside the site, as the local
target has it.

**The debug session and the runbooks on a host.** On Kubernetes three things make the
debug session safe. Each needs an equal on a host:

| Guard | Kubernetes | Host |
|---|---|---|
| The verbs are fixed | The core permits four `kubectl` verbs | The runner has the reads in its code. A helper on the host accepts only the reads `logs` and `events`, and it treats what the runner sends as untrusted input |
| The scope is added by the core | `-n <namespace>` | The hosts come from the approved `targets`. The helper has a list of the units and the log files that it can read |
| The credential is limited and short-lived | A token for a role in one namespace | See the options |

| Option | How | For | Against |
|---|---|---|---|
| 1. SSH certificate (recommended) | The broker signs a user certificate that is valid for minutes, with one principal for the environment and no permission but a forced command. The host trusts the key of the broker, lists the principal, and has a rule for the user that forces the helper and switches off each kind of forwarding | The same on every cloud and in a data centre. It fits the grant model and the `Issuer` interface of the broker. The rule is on the host, so a faulty runner or a wrong certificate cannot pass it | A certificate cannot be withdrawn; it expires, unless each host keeps a revocation list. The host and the broker need the same clock. The broker holds a signing key. To read the journal, the user is in a group that reads the logs of every unit, so the helper must filter by unit |
| 2. The command service of a cloud | AWS Systems Manager Run Command with a document whose parameters have a list of permitted values, and a role that is limited to that document. On Azure, an Automation runbook on a Hybrid Runbook Worker: the script is fixed in the account, and the caller sends parameters and the name of a group of workers | No inbound port. On AWS the audit trail records each command; on Azure the record of the job has the caller and the parameters. It needs no change of the broker. Azure Arc brings the Azure service to a server in a data centre | A full equal on AWS only. On Azure the service does not check the parameters, the caller selects the group of workers, and a worker asks for jobs each 30 seconds. Azure Run Command runs any script that the caller sends, as root. Google Cloud has no command on request: OS policies and patch jobs are not for reads, and OS Login and IAP give a login. The service cuts long output |
| 3. A reporter on the host | A small read-only service on the host. The core reads it over HTTPS | The simplest network model on a cloud | The debug session has no grant and no credential of its own. The core must reach the host, so it does not work for a site |

Only option 1 works for a server in a data centre with no agent of a cloud. The SSH
certificate services of Azure and of Google Cloud cannot take the place of the key of the
broker: their documents show no forced command and no principal that the caller sets.

**The host kit.** A host of an application gets two things: the collector, and the helper
for the debug session. With the users, the trust in the key of the broker and the lists of
the helper, they are the "host kit". The core owns it, as it owns the roles of the
identities on Kubernetes. A pack does not write it.

- On hosts that a layer creates, the layer of the platform installs the kit.
- On hosts that exist, an administrator installs it one time with their own credential,
  at `clanker setup`. `docs/PLAN.md` section 6.4 has the same step for a cloud account.
- The kit comes first, with a configuration that reads nothing and empty lists.
- The configuration of the collector and the lists of the helper come from the manifest.
  They are a part of the first deployment, a tier T3 change, so a person approves them on
  the approval page before a host gets them.

**The identities on a host.**

| Identity | On a host |
|---|---|
| `observer` | The user that the collector runs as. It can read the state of the units, the file systems, the permitted version files and the logs that a `log-match` check counts. It has no certificate: nothing connects to the host for it |
| `debugger` | A user that can run the reads of the helper. Its certificate needs the debug grant |
| `runbook` | A user that can run the commands that the kit permits for the units of the pack that are not shared, for example a restart |
| `applier` | A user for the layer of the application, with a grant for that layer as today |

**Runbooks.** A script gets the credential of the runtime of the components that its
definition names: `KUBECONFIG` for `kubernetes`, as today, and for `systemd` an SSH
configuration file in which each instance is a host alias.

Sources: the documents and the metadata of each component in the repositories of the
collector project at the versions of section B.3, and the specifications of OTLP and
OpAMP; the manual pages `ssh-keygen(1)`, `sshd_config(5)` and `sshd(8)` of OpenBSD; the
manuals of `journalctl` and of the D-Bus interface of systemd; the Kubernetes documents on
the authorization of the kubelet and on good practice for RBAC; the AWS Systems Manager
user guide; the Azure documents of Run Command, Arc, Automation and the sign-in with
Entra; the Google Cloud documents of OS Login, IAP, OS Config and patch jobs.

Not checked: the size and the memory use of the build of the collector; the group that
the journal receiver needs; that the check receivers send one request for each interval;
that sshd permits no difference of the clocks; that the Azure roles limit a principal to
one runbook; that a service account can get an OS Login certificate.

### B.8 The health rules

The rules stay in the core, with the same limits as today. They read the observation.

**Samples.** The rules of today count requests in a sample. A check of the core gives
that directly. A check of a collector gives one result for each interval, so the core
makes a sample from the results of one sample period. The core sets the interval of the
primary check, so that a sample period has 8 results or more, as a sample has today.

| Rule | Today | Proposed |
|---|---|---|
| Canary after an apply | The probe has under 10 % errors of 40 requests | The primary check has under 10 % errors of 40 results. Each other check passes. Each required component is `ok`. Each version that the source gives is the wanted version: one for each instance, or one for the application with `http-json` |
| Incident | Over 20 % errors in 3 samples, when no change applies and no incident is open | The same for the primary check, **or** a required component is `down` or `degraded` in 3 reports of its collector in a row. The two conditions of today stay, so a rollout opens no incident |
| Not reachable | — | A required component is `unknown` for 3 intervals of its collector: an incident with the title "not reachable". A primary check that is `unknown` opens no incident for errors |
| Recovery | 20 of 20 requests succeed | The primary check passes in 20 of 20 results, and each required component is `ok` |

- The canary and the recovery read only data that is newer than the apply or the
  mitigation. The collector sends at its own interval, so the wait is longer than the 90
  seconds of today. The core gives the wait a limit from that interval. When the limit
  ends with too few results, the canary or the recovery fails.
- An observation is the newest data of each collector at a monitor interval. A rule that
  counts reports counts each report of a collector one time.

For a primary check of the kind `job`:

- The canary cannot wait for the next run. It passes on the state of the components and
  the version, and its text says that no run happened since the apply.
- An incident opens when the check fails in one observation: a run failed, or no run
  happened in time.
- The incident is recovered when a later run passes.

A pack does not set a limit of these rules. A limit that a pack can loosen can hide an
incident or pass a faulty release. Section B.13 has the alternative.

### B.9 Mitigation

- A mitigation runbook has exactly one input, `component`, with the components that it
  permits as values. `pack.Load` refuses a mitigation runbook without it, a value that is
  not in `components`, and a value that is a shared component.
- The field `deployment` in the answer of the Investigator becomes `component`.
- `incident_deployment` becomes an optional `incident: {"runbook": …, "component": …}`: the
  proposal when no model gives a diagnosis. Without it, the core proposes nothing and the
  incident goes to a person. This is right for an application of jobs, where a restart
  means nothing. The core no longer has the name of a runbook in its code.
- A runbook definition gets a field `title`: one to five words with a fixed pattern. The
  titles and the write-up use it, not the word "Restart". The description of a runbook is
  free text, so it does not go into a title.

### B.10 The manifest, schema 2

Schema 2 replaces schema 1. The loader reads one schema.

- The first step of section B.14 makes the change, with the Paperboat pack and the test
  pack in the same pull request.
- A later step adds a runtime, a check kind or an optional field to schema 2. A core
  that does not have it refuses the pack, which is correct.
- Schema 2 is for a new environment. A pack cannot change in an environment that runs,
  and the standing approvals name the runbooks of the first start
  (`internal/orchestrator/runbook.go:19`). The table of the health history changes with no
  migration, so an environment of the demonstration starts again.

| Schema 1 | Schema 2 |
|---|---|
| `deployments`, `namespace` | `components`; the layer output `targets` |
| `data_namespace` | An entry of `targets`. It is a fact of the target, not of the application: only a target with its data services in the cluster has one |
| `incident_deployment` | `incident`, optional |
| `debug_commands` | `diagnostics` |
| `health.*` | `checks`, `app_version` |
| — | `volumes` |
| `chart`, `mount`, `build` (required) | `deploy: {"kind": "helm", "chart": …, "mount": …}` or `deploy: {"kind": "layer"}`, where the layer of the pack does all of the deployment. `build` is optional with both |
| `topology.services` | `components`, plus `topology.extra` for nodes that are not components (the load generator, a load balancer) |

The part that changes, for the two applications of section B.11:

```json
"components": [
  {"name": "api", "runtime": "kubernetes", "workload": "paperboat-api", "required": true},
  {"name": "web", "runtime": "kubernetes", "workload": "paperboat-web", "required": true},
  {"name": "worker", "runtime": "kubernetes", "workload": "paperboat-worker", "required": true}
],
"checks": [
  {"name": "ready", "kind": "http", "path": "/api/healthz"},
  {"name": "inventory", "kind": "http", "path": "/api/inventory", "primary": true}
],
"app_version": {"source": "http-json", "path": "/api/version"},
"diagnostics": [
  {"read": "logs", "component": "api", "lines": 30},
  {"read": "events"}
],
"incident": {"runbook": "restart-component", "component": "api"}
```

```json
"components": [
  {"name": "app", "runtime": "systemd", "unit": "ledger-app.service", "required": true},
  {"name": "proxy", "runtime": "systemd", "unit": "nginx.service", "required": true,
   "shared": true, "log_path": "nginx/ledger-error.log"},
  {"name": "export", "runtime": "systemd", "unit": "ledger-export.timer", "required": false}
],
"checks": [
  {"name": "login", "kind": "http", "path": "/login", "primary": true},
  {"name": "app-port", "kind": "tcp", "port": 8443, "from": "host", "component": "app"},
  {"name": "app-errors", "kind": "log-match", "component": "app",
   "pattern": "ERROR|OutOfMemoryError", "limit": 5},
  {"name": "export-ran", "kind": "job", "component": "export", "max_age_hours": 26}
],
"app_version": {"source": "file", "component": "app", "path": "ledger/VERSION"},
"volumes": [{"name": "app-data", "component": "app", "path": "ledger/data"}],
"diagnostics": [
  {"read": "logs", "component": "app", "lines": 60},
  {"read": "logs", "component": "proxy", "lines": 20}
],
"incident": {"runbook": "restart-component", "component": "app"}
```

### B.11 Paper test: two applications through each interface

Paperboat is the example pack. "Ledger" is an invented legacy application on two servers
that exist already in a data centre: a Java service, a proxy that serves other sites too,
and a timer that writes an export at night. The database is on a third server. It has no
health endpoint and no version endpoint. The core runs on a cloud and has no network path
into the data centre.

| Interface | Paperboat | Ledger |
|---|---|---|
| Components | Three deployments | Two services, one of them shared, and one timer |
| Where they run | `targets`: one namespace, from the layer | `targets`: two addresses for each component, as variables of the site layer that a person approved |
| Collector | One in the cluster and one on each node, from the layer of the platform | One on each server, and one in the site for the check from outside |
| Primary check | `GET /api/inventory`, from the core | `GET /login` through the proxy, from the collector of the site |
| Other checks | `GET /api/healthz` | The port of the service on each host; the count of error lines in its journal; the export of the night |
| Running version | `GET /api/version` | The file `VERSION` on both hosts |
| State | Pods ready of pods wanted | Active on 2 of 2 hosts; the last run of the timer |
| Volumes | The volume claims | The file system of the data directory |
| Logs and events | Pod logs and namespace events, in a debug session | The journal of the service and the log file of the proxy for this site, in a debug session |
| Runner | Started for each job | The site runner in the data centre |
| Credential of the debug session | A token for `view` in the namespace | An SSH certificate for the helper |
| Who installs the guard | The broker binds the roles | An administrator installs the host kit at setup |
| Mitigation | `restart-component` with `kubectl rollout restart` | `restart-component` with `systemctl restart` on one host after the other. The proxy is shared, so no runbook can restart it |
| Deployment code | A chart that the layer installs | The layer of the pack copies a package to the servers |
| A full volume | The finding proposes a larger size | The finding goes to a person |

The fields that have a meaning for one runtime only are those that say how a runtime
finds a thing: `workload`, `unit`, `log_path`, `claim`, `path`, and the version sources
`file` and `image`.

Four more invented applications were put through the first draft of this design. The
draft handled none of them without a problem. The table shows what this version does
with each.

| Application | Needed | In this version |
|---|---|---|
| PHP under Apache with MySQL on one machine, deployed as a copy of files | Logs in files; a web server that serves other sites; a database that listens on its host only | `log_path`, `shared`, a check `from` `host`. The deployment must write the version file last |
| Jobs at night with no listener | A primary check with no request; no restart | The check kind `job` as primary; no `incident` |
| Containers of Docker Compose on one host | The state of each container; the image as the version | Not until the runtime `docker` exists (section B.13) |
| Java on an application server behind a load balancer, with a queue, on shared hosts | An incident when one host of two is dead; the shared server | The rule for `degraded`; `shared` |

### B.12 What bare metal in a data centre needs

This section collects what the other sections say for a server that exists already, with
no cloud interface.

| Question | Answer |
|---|---|
| Where do the hosts come from? | Variables of the site layer, approved by a person (B.4) |
| How does the data reach the core? | A collector on each server sends it to the collector of the site, which sends it to the core over outbound HTTPS (B.7) |
| Where do the checks run? | In the collector of the site and in the collectors on the hosts (B.5) |
| Where do the jobs run? | In a site runner with outbound HTTPS only (B.7) |
| What credential does the broker give? | An SSH certificate. No identity service of a cloud is needed (B.7) |
| Who installs the collector, the helper and the trust on the hosts? | An administrator, one time, at setup (B.7) |
| What is the fix for a full disk? | A finding for a person. A disk does not grow from a variable (B.6) |
| What does the core do when the site is not reachable? | The states are `unknown`. An incident "not reachable" opens, and no incident for errors. The collectors keep their data and send it later (B.7, B.8) |

Three things must exist before a pack can deploy on such servers, and this design does
not define them:

- A fifth target in `platform/CONTRACT.md` for hosts that exist, with its own conformance
  suite. The contract of today assumes a cloud or a cluster in its state backends, its
  identities and each of its six conformance checks.
- An application layer that the pack brings (`docs/PLAN.md` section 5.7, "Not built").
- A source for packages inside a site whose hosts have no outbound path.

### B.13 Decisions for the owner

Decided:

| Decision | Choice |
|---|---|
| What gathers the standing data | **Chosen 2026-11-20 by the owner:** the OpenTelemetry Collector, with a configuration that the core writes (B.3) |
| Charts of runtime health | **Chosen 2026-11-20 by the owner:** charts of load, CPU, memory, disk space, network and disk operations for each host and node (B.16) |
| Where the series of the charts are | **Chosen 2026-11-20 by the owner:** a bounded table in the database of the control plane. This document recommends no extension of PostgreSQL for it (B.16) |
| Detectors on those series | **Chosen 2026-11-20 by the owner:** not now. The first version has charts only |

Open:

| Decision | Recommended | Alternative |
|---|---|---|
| The logs of the application | The collector sends no log line. The Investigator reads logs in a debug session under a grant (B.7) | The collector sends all logs to the core. The core becomes a store for logs, the helper on the host is not needed, and the rule "no standing access for the debugger" ends |
| The collector on Kubernetes | The same collector there too, in the layer of the platform, so that one path gives the observation | The code of the project reads the cluster, and the collector is for hosts only |
| Where `log-match` counts | In the collector on the host; the pattern is in its configuration (B.5) | No `log-match` check |
| The configuration of a collector | A file in the first deployment, approved by a person (B.7) | The remote management protocol of the collector project, when its parts are stable |
| Access to a host for a debug session and a runbook | An SSH certificate from the broker and the host kit (B.7, option 1) | AWS Systems Manager for hosts on AWS and an Azure Automation runbook for hosts on Azure, with the certificate for the rest |
| The runner of a site | A runner that stays, is enrolled one time and asks the core for jobs (B.7) | A runner that an administrator starts for each job |
| Who installs the host kit on hosts that exist | An administrator, at `clanker setup` | The first deployment, with a credential that the administrator gives for that one job |
| Runtimes after Kubernetes | `systemd` first, then `docker` for containers on a host with no orchestrator | Only `systemd`. Windows services are not in this design |
| Health limits | Fixed in the core (B.8). A pack sets only what one check is | A pack sets the limits of the rules inside a range that the core gives |
| The version source `layer` | Permitted, and shown on the approval page and in each canary | Refused: each pack must have a version that the core can read |
| Schema change | Schema 2 replaces schema 1, for new environments (B.10) | A migration of an environment that runs |
| Hardware of a server (disks, the RAID, power, the management controller) | Not in this design. No detector uses it, and it belongs to the platform | Receivers of the collector for it, as a part of the target for hosts that exist |
| Data from a monitoring system that the application has already | Not in this design | The collector reads it: it has receivers for the common formats |
| Where the text is that says when a mitigation fits | In the prompt of the core, in general words, while a pack has one kind of mitigation | A checked field of the runbook definition |
| Names of the fields of the observation | The names of this document. No public convention has this record: OpenTelemetry has names for the identity of a service, a host and a pod, and no values for health. The health check format of the IETF is a draft that expired in 2022 | The OpenTelemetry names for the identity fields, when the record goes to another system |

### B.14 Build order

Each step is one pull request or a few, and each leaves Paperboat working.

1. **Components, on Kubernetes only.** Schema 2 with `components`, `incident`,
   `diagnostics`, `checks` of the kind `http`, the `http-json` version and `deploy` of the
   kind `helm`. The output `targets` in the layer `40-platform`, and the rights of the
   identities from it. The job type carries reads. The Investigator, the eval cases, the
   checks of the eval suite and the recorded answer of the Investigator change to
   components. The evals run again.

   In this step the core puts two reads of its own before the reads of the pack in a
   debug session on Kubernetes: the state of the pods and of the deployments. The
   observation of step 2 takes their place.

   Built so far: the manifest of schema 2 with `components`, `incident`, `diagnostics`,
   `checks`, `app_version` and `deploy`, and the code of the core that reads it. The
   job of a debug session carries reads, and the runner makes the commands. The
   namespaces are not in the manifest: the layer `40-platform` has the output `targets`,
   and the debug session and the rights of the identities read it. On Kubernetes the
   output is a map with the key `application` for the namespace of the components and
   the key `data` for the namespace of the data services; a list for each component
   comes with a runtime on hosts. The runbooks, the prompt of the Investigator and the
   eval cases name components now. A mitigation runbook has a `title` and the one input
   `component`, and the prompt gets the components and the mitigation runbooks of the
   pack through placeholders. The evals of the Investigator ran with the live models
   after the change of the prompt, with the same scores as before.

   The approval page has no own display of the targets. On Kubernetes the targets come
   from the variable `app_namespace` of the layer `40-platform` and from the code of the
   layer `30-data`, and a change of either is a change of the platform repository, which
   the approval page shows as a diff and a plan. An own display comes with the runtime
   on hosts, where the targets are a list of addresses. With this, step 1 is complete.
2. **The collector on Kubernetes.** The build of the collector. The endpoint of the core
   and the observation. The generator of the configuration. State, versions and volumes
   of Paperboat. The health history by name. The storage detector reads real samples.
3. **The rules of B.8**, the `tcp` check, the `image` version, the state `unknown`.
4. **The site runner.** Enrolment and the jobs of a site.
5. **Access to a host.** The certificate in the broker, the host kit with the collector
   and the helper, and a test machine that runs systemd.
6. **Hosts.** The receiver of the project, checks from a site and from a host,
   `log-match`, `job`, the `file` version, and a second example pack on that machine. This
   step needs the target for hosts that exist and the layer of that pack (section B.12).
7. **The `docker` runtime**, with the receiver of the collector for containers, which is
   alpha.

The charts of section B.16 come in two parts. The table, the read of the API and the
panel for the nodes of Kubernetes come after step 2. The charts for hosts come with
step 6.

### B.15 Not in this design

- Several packs in one environment, also two packs on one host, and a new version of a
  pack for an environment that runs (`docs/PLAN.md` section 5.7, "Not built").
- The scenes of the simulator, which are about Paperboat.
- Traces, and metrics of the application other than the checks: the depth of a queue, the
  heap of a Java process, a pool of connections. The collector can gather them; no
  detector reads them.
- The state of a load balancer that is hardware, and of its members.
- Jobs of plain cron with no unit. A pack needs a timer.
- An application server that starts its own managed servers, for example with a node
  manager.
- The hardware of a server, the installation of its operating system, and the end date of
  a certificate.
- Windows hosts.
- The nodes of the platform in the topology view for a target with no cluster.

### B.16 Runtime health in the dashboard

**Decided 2026-11-20 by the owner:** the dashboard shows the runtime health of each host
and each node as charts. The data is in a bounded table of the database of the control
plane. The first version has charts only: no detector reads this data.

This changes one statement of the first version of this design, that the core is no
store for metrics. The core now keeps one more thing than the observation: a fixed list
of series for each host and node. It is still not a general store: a pack adds no series,
and the core drops each metric that is not in the list.

**The series.** They are for a host or a node, not for a component.

| Series | On a host | On a node of Kubernetes |
|---|---|---|
| Load: the average of 1 minute, and the number of CPUs | Receiver `hostmetrics`, scraper `load` | The same scraper, in the collector of the node |
| CPU: the part that is in use | Scraper `cpu` | Receiver `kubeletstats`, or the scraper `cpu` |
| Memory: used and total | Scraper `memory` | Receiver `kubeletstats`, or the scraper `memory` |
| Disk space: used and total, for each file system | Scraper `filesystem` | Receiver `kubeletstats` for the file system of the node |
| Network: bytes in and out for each second, and errors | Scraper `network` | Receiver `kubeletstats`, or the scraper `network` |
| Disk operations: reads and writes for each second | Scraper `disk` | Scraper `disk`. The receiver `kubeletstats` has no such metric |

- The receiver `hostmetrics` is beta. In a container it reads the host through a mount of
  the file system of the host, so the collector of a node needs that mount, read-only.
- The collector sends the network bytes, the disk operations and the CPU time as counts
  that grow. The core stores the rate between two reports.
- The network is the sum of the interfaces without the loopback interface. The disk
  operations are the sum of the devices. Disk space is for each local file system, 16 for
  each host at most.
- These are settings of the configuration that the core writes already. No value of a
  pack is in them.

**The table.** One row is a time, an environment, an instance, a series and a value.

| Level | One point for each | Kept for |
|---|---|---|
| Raw | Minute | 48 hours |
| Rollup: the average and the maximum | 5 minutes | 30 days |

- With 20 instances and 15 series, the raw level has under 1 million rows and the rollup
  under 3 million. The core makes the rollup and removes old data.
- The limits are in the core: the list of series, the number of instances of an
  environment (those of the approved targets), and the two periods.

**PostgreSQL needs no extension for this.**

- The table is partitioned by day with the partitions that PostgreSQL has itself. The
  core makes the partition of a day before it writes the first row of that day, and drops
  the partition that is too old. The clock of the core can jump in a demonstration, so
  the core does not make partitions on a schedule. To drop a partition costs almost
  nothing; to delete rows leaves work for the database.
- The time column is a timestamp, not text as in the tables of today, so that the
  partitions work.
- An extension for time series (TimescaleDB) can do the partitions, the rollup and
  compression itself. The design does not use it: a managed database of a cloud does not
  always offer it, and the control plane must run on each target with the same database.
  Not checked: which managed services offer it today.
- An extension that makes partitions on a schedule (`pg_partman`) is not needed, because
  the core does that work.

**The dashboard.** The API gets one read: the series of an environment between two times,
at the level that fits the range. The dashboard has a chart library already
(`@carbon/charts-react`) and a health chart on the overview page
(`web/src/pages/overview/HealthChart.tsx`).

- A panel "Runtime" for each environment, with one row of small charts for each host or
  node: load, CPU, memory, disk space, network, disk operations.
- The charts show the marks of the work items of the timeline, so that a person sees a
  change of memory or of load beside the deployment that came before it.
- A host or a node with no data in time shows a gap and the state `unknown`, not a zero.

**The Investigator.** The observation of a debug session gets the last hour of the series
of the hosts or nodes of the application. They are numbers, so they need no scrub.

**Later, not in this version.**

- Detectors on these series, for example for memory that grows.
- Series for each component: the CPU and the network of one unit or one pod need more
  receivers, and the disk operations of one pod have no source in `kubeletstats`.
- A copy of the data to a monitoring system that the site has already.
