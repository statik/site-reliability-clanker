# Application packs — design

This document has two parts. Part A says how an application pack works today and what the
core requires from it. Part B is a proposal: a standard way to gather health and
observability data, so that a pack can describe a legacy application that does not run on
Kubernetes, also on servers in a data centre. Nothing in part B is built, and nothing in
part B is decided. Section B.13 has the decisions for the owner.

`docs/PLAN.md` section 5.7 has the rules of a pack. `apps/paperboat/PACK.md` has the fields
of the manifest and the commands.

## Part A — How a pack works today

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

Not a goal: a general monitoring system. The core gathers only what its detectors and the
Investigator use.

### B.2 Terms

| Term | Meaning |
|---|---|
| Component | One part of the application that runs: a deployment, a service of the operating system. It replaces "deployment" in the manifest, the runbooks, the prompts and the eval cases |
| Instance | One copy of a component: a pod, or the unit on one host |
| Runtime | The kind of system that runs a component. The core has a closed list of runtimes |
| Collector | The code of the core that reads one runtime. Each runtime has one |
| Check | A test of the application: a request, a connection, the result of a job |
| Observation | The record that the collectors and the checks produce. It has one format for every runtime |
| Site | A network that the core cannot reach from outside: a data centre, a private network |
| Site runner | A runner that stays inside a site and gets its jobs from the core |

### B.3 The pack declares, the core collects

A pack can bring data for collection in two ways.

| | A. Declared (recommended) | B. Scripts |
|---|---|---|
| The pack brings | Names and numbers: the components, their runtime, the checks | Programs that read the application and print the observation |
| The core runs | Its own collector for the runtime, with commands that are in its code | The program of the pack, with a read-only credential |
| A new kind of application needs | A new runtime in the core, when no runtime fits | Only a new pack |
| What a pack can make the runner do | Nothing outside the fixed reads | Any read that the credential permits, and any use of the network |
| Output that a model reads | Produced by the core | Produced by the code of the pack |

Way A keeps the rule that the core fixes the verbs. It is the same step that the debug
commands took, one level higher: today the pack gives the words after a fixed verb, and
with way A the pack gives only the name of a component and a number of lines. Way B is
more flexible, and it makes each pack a program that needs a review before each import.

When no runtime fits an application, the answer is a new runtime in the core, with its
tests. It is not a script in a pack.

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

**The reads.** A collector has five reads.

| Read | `kubernetes` | `systemd` |
|---|---|---|
| `state` | The workload and its pods: ready and wanted instances, restarts, the time of the last start. With no component: each pod of the namespace, so that a pod of a data service is in the record | Named properties of the unit on each host: the active state, the result, the restart count, the time of the last start. For a timer: the last run and the result of the service that it starts |
| `logs` | The last lines of the pods of the workload | The last lines of the journal of the unit, or of `log_path` |
| `events` | The events of the namespace that name the workload or one of its pods | The journal lines of the unit with the priority warning or higher, and the lines of systemd about a start or a stop of the unit |
| `saturation` | Memory of the pods; the fill level of the volumes of the pack | Memory of the unit; the fill level of the volumes of the pack |
| `version` | Not used | The first line of the version file (section B.5) |

- The `systemd` collector asks for named properties only: the load state, the active
  state, the sub state, the result, the restart count and the start time of the main
  process. The full properties of a unit can hold its environment variables, and those
  can hold a secret.
- The active state alone is not enough. A service that fails and starts again in a loop
  reports "activating", so the collector reads the sub state too.
- On Kubernetes, the memory of a pod and the fill level of a volume are not in the core
  interface of the cluster. They come from the metrics service and from the statistics of
  a node. A role in one namespace probably does not give the statistics of a node; this
  was not checked. If it does not, the identity `observer` needs a read right outside
  the namespace, and a debug session has no `saturation` read on Kubernetes.
- The exact commands are fixed in the implementation, in the collector, with a test for
  each.

**The states.** Each collector maps the states of its runtime to four.

| State | `kubernetes` | `systemd` service | `systemd` timer |
|---|---|---|---|
| `ok` | Each wanted instance is ready and has the new specification | Active and running on each host, with no new restart | The timer is active: it waits for its next run |
| `degraded` | One instance or more is ready, but not all; or a container started again since the last observation | Active on some hosts; or it starts, stops or reloads; or it started again since the last observation | Active on some hosts |
| `down` | Instances are wanted and none is ready | Failed, inactive, or in a loop of restarts, on each host | The timer is not active |
| `unknown` | The collector got no answer, or the cluster did not yet read the last change | The collector got no answer from a host, or the unit is not there | The collector got no answer |

- `unknown` is a real value. The detectors do not read `unknown` as `ok`.
- The four states are those of a long convention of monitoring programs: OK, warning,
  critical, unknown.
- A timer and the service that it starts are two units. The collector reads both. The
  state of a timer says only that it will run. The result of its last run is the check
  of the kind `job` (section B.5), so a timer that had no run since an apply is `ok`.
- A service that runs one time and ends is healthy when it is inactive and its result is
  a success. The collector must know this type of unit from its properties.

**Volumes.** The manifest gets a list `volumes`: a `name`, a `component`, and `claim` (the
volume claim, for `kubernetes`) or `path` (a file system path, for `systemd`). An optional
`size_variable` names the variable of the layer that sets the size.

**Diagnostics.** The debug session of a pack becomes a list of reads. It replaces
`debug_commands`:

```json
"diagnostics": [
  {"read": "state"},
  {"read": "logs", "component": "api", "lines": 30},
  {"read": "events"}
]
```

A read has the name of one of the reads `state`, `logs`, `events` and `saturation`,
optionally one component of the pack, and `lines` from 1 to 200. There is nothing else to
check: no flag, no word, no format.

### B.5 Checks

`health` becomes a list `checks`. Each check has a `name` with the pattern of a component
name, a `kind`, and the fields of its kind.

| Kind | Fields | Passes when |
|---|---|---|
| `http` | `path`, optional `status` (default 200), optional `from`; with `from` `host` also `component` and `port` | The answer has the status |
| `tcp` | `address_output`; or, with `from` `host`, `component` and `port` | The connection opens |
| `log-match` | `component`, `pattern`, `limit` (0 to 100) | The log lines of the component since the last observation have `limit` lines or fewer that match the pattern |
| `job` | `component` (a timer), `max_age_hours` (1 to 168) | The last run passed and is not older than the limit |

**Where a check runs.** `from` has two values.

| `from` | The check runs | Against |
|---|---|---|
| `outside` (default) | In the core. For an environment in a site, in the site runner | The URL of `url_output`, or each address of `address_output`: an output of the layer with a list of `host:port` values, under the address rules of section B.4 |
| `host` | In the `observe` job, on each instance of `component` | `localhost` and `port`. For a database that listens only on its host, or a port behind a load balancer |

A check that could not run has the state `unknown`. It is not a failed check. Today a
request with no network path counts as an error (`internal/monitor/monitor.go:93`).

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

- The `observe` job reads the lines of the component after the position of the last
  observation, 2000 lines for each instance at most. The position is a cursor of the
  journal, or an offset in a log file. The core keeps it for each instance and gives it
  to the job.
- The core applies the pattern to the lines and keeps only the count. The pattern goes to
  no program.
- The pattern is a regular expression of the Go syntax, 200 characters at most. The Go
  package guarantees a run time that is linear in the size of the input.

**The version that runs** comes from `version`, with one of these sources.

| Source | Fields | The version is |
|---|---|---|
| `http-json` | `path` | The field `version` of the JSON answer. This is the source of today. It gives one value for the application, in the field `version.running` of the observation, not one for each instance |
| `file` | `component`, `path` | The first line of a file on each host of the component. The path is relative to a directory that the host permits (section B.7). The helper refuses a symbolic link that leaves that directory |
| `image` | `component` | The tag of the image of each instance. For `kubernetes`, and for `docker` |
| `layer` | none | The output of the layer. The control plane then knows only what it deployed, not what runs |

- A value that does not match the pattern of a version is `unknown`.
- Instances of one component that report different versions are a failed check.
- With the source `layer`, no running version is checked. The approval page of the first
  deployment and the text of each canary say so, and `clanker pack validate` prints it.
  Section B.13 has the alternative: refuse the source.

### B.6 The observation

Every collector and every check writes into one record. This is the standard.

```json
{
  "schema": 1,
  "env": "prod",
  "at": "2026-11-20T10:15:00Z",
  "application": "ledger",
  "version": {"wanted": "4.2.1", "source": "file"},  "components": [
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
- `logs.text` and `events.text` hold text of the application. The core scrubs them and
  frames them as untrusted data, as it does with the transcript today.
- No other field holds free text. A version must match the pattern of a version. An
  instance has no field for a message of the runtime: such a message goes into `events`.
- `restarts` is the number of restarts since the last observation. The runtimes count
  restarts in different ways, and a count can go back to zero.
- `started` is the time at which the main process of the instance started last.
- A part that was not gathered is absent. A monitor sample has no `logs`.
- An entry for memory has the form of a volume entry, in its own list. `total` is absent
  when the component has no limit. CPU is not in the record: no detector uses it.

Who reads it:

| Reader | Today | With the observation |
|---|---|---|
| Health history | `ok`, `total`, `p95_ms`, `version` of one probe | One row for each check and each component state, by name |
| Incident detector | Error rate of the probe | Section B.8 |
| Storage detector | Samples that only the simulator writes | `volumes`. It proposes a larger size only for a volume with a `size_variable`. For a disk of a server it opens the finding for a person and proposes no change |
| Investigator | The text of `kubectl` commands | The observation of the debug session: the state as the core printed it, the logs and the events as scrubbed text |
| Topology view | A health value on the node `api` | The state of each component |
| Eval cases | `kubectl` transcripts | Observations. `expect` becomes `<runbook>:<component>`. The format of an eval case file gets the version 2, and the checks of the suite name no runbook |

The Investigator prompt then speaks of components in the state `ok`, not of pods that are
"Running and Ready", and it reads the list of runbooks that are mitigations from the
definitions, not from a name in its text. The prompts change, so the evals of every role
that changes run again before the change merges.

### B.7 Where collection runs, and with which identity

The core starts no program (`docs/PLAN.md` section 12, "Core and runners"). So:

| Data | Runs in | Identity |
|---|---|---|
| Checks `from` `outside`, the `http-json` version | The core. For a site: the site runner | None: a network request |
| `state`, `saturation`, the `file` and `image` versions, checks `from` `host`, the lines for `log-match`, the `job` check | A runner, as a new job kind `observe` | `observer`: standing, read-only (`docs/PLAN.md` section 6.1) |
| The reads of `diagnostics` for the Investigator | A runner, as the job kind `diagnose` | `debugger`: no standing access, a grant of 15 minutes, the session recorded |

**When `observe` runs.**

- On a slow schedule, for example each 5 minutes.
- At each monitor interval while the primary check fails or a required component is not
  `ok`. The "3 samples" of section B.8 are then 3 monitor intervals.
- In the check after an apply, and in the check for recovery. Both rules need the state
  of the components and the version of each instance at that moment.

The job type changes with this: `Commands` (kubectl argument lists) becomes a list of
reads, and `KubeConfig` becomes a credential with a kind.

**The site runner.** A runner inside a site needs a network path to the hosts and outbound
HTTPS to the core. The direction of the protocol fits already: each call goes from the
runner to the core with a token (`internal/runner/client.go:35`). What is missing:

- A runner that stays. Today the core starts a runner for each job, and only as a process
  on its own machine (`internal/launch/local.go:31`). A site runner is enrolled one time,
  has an identity for its site, and asks the core for the jobs of its environments. This
  changes the choice "Core and runners" of `docs/PLAN.md` section 12, which says that a
  runner is short-lived and that the core starts it.
- A time limit for a job that no runner takes.
- The checks `from` `outside` of an environment in a site run in this runner.

A site with no outbound path to the core needs the core inside the site, as the local
target has it.

**Access to a host.** On Kubernetes three things make the debug session safe. Each needs an
equal on a host:

| Guard | Kubernetes | Host |
|---|---|---|
| The verbs are fixed | The core permits four `kubectl` verbs | The collector has the commands in its code. A helper on the host accepts only the reads, and it treats what the runner sends as untrusted input |
| The scope is added by the core | `-n <namespace>` | The hosts come from the approved `targets`. The helper has a list of the units, the log files and the directories that it can read |
| The credential is limited and short-lived | A token for a role in one namespace | See the options |

| Option | How | For | Against |
|---|---|---|---|
| 1. SSH certificate (recommended) | The broker signs a user certificate that is valid for minutes, with one principal for the environment and no permission but a forced command. The host trusts the key of the broker, lists the principal, and has a rule for the user that forces the helper and switches off each kind of forwarding | The same on every cloud and in a data centre. It fits the grant model and the `Issuer` interface of the broker. The rule is on the host, so a faulty runner or a wrong certificate cannot pass it | A certificate cannot be withdrawn; it expires, unless each host keeps a revocation list. The host and the broker need the same clock. The broker holds a signing key. To read the journal, the user is in a group that reads the logs of every unit, so the helper must filter by unit |
| 2. The command service of a cloud | AWS Systems Manager Run Command with a document whose parameters have a list of permitted values, and a role that is limited to that document | No inbound port. The audit trail of the cloud records each command. It needs no change of the broker | AWS only. Azure Run Command runs any script as an elevated user, and no equal was found on Google Cloud. None in a data centre. The service cuts long output |
| 3. A reporter on the host | A small read-only service on the host. The core reads it over HTTPS | The simplest network model on a cloud | A process that always runs on a legacy host. The debug session has no grant and no credential of its own. The core must reach the host, so it does not work for a site |

Only option 1 works for a server in a data centre. The sources for this table are at the
end of the section.

**Who installs the guard.** The helper, the users, the trust in the key of the broker and
the lists of the helper are the "host kit". The core owns it, as it owns the roles of the
identities on Kubernetes. A pack does not write it.

- On hosts that a layer creates, the layer of the platform installs the kit.
- On hosts that exist, an administrator installs it one time with their own credential,
  at `clanker setup`. `docs/PLAN.md` section 6.4 has the same step for a cloud account.
- The kit comes first, with empty lists: the helper then reads nothing.
- The lists of the helper (the units, the log files, the directories) come from the
  manifest. They are a part of the first deployment, a tier T3 change, so a person
  approves them on the approval page before the helper gets them.

**The identities on a host.**

| Identity | On a host |
|---|---|
| `observer` | A user that can run the reads `state`, `saturation` and `version`, the checks `from` `host`, and the log read for `log-match`. Its certificate is signed for each `observe` job, with no grant, because the access is standing by design |
| `debugger` | A user that can run each read. Its certificate needs the debug grant |
| `runbook` | A user that can run the commands that the kit permits for the units of the pack that are not shared, for example a restart |
| `applier` | A user for the layer of the application, with a grant for that layer as today |

**Runbooks.** A script gets the credential of the runtime of the components that its
definition names: `KUBECONFIG` for `kubernetes`, as today, and for `systemd` an SSH
configuration file in which each instance is a host alias.

Sources for the options: the manual pages `ssh-keygen(1)`, `sshd_config(5)` and `sshd(8)`
of OpenBSD; the manuals of `journalctl` and of the D-Bus interface of systemd; the AWS
Systems Manager user guide (document parameters, Run Command set-up); the Azure
documentation of Run Command; the Google Cloud documentation of OS Login. Not checked:
that sshd permits no difference of the clocks, and that Azure and Google Cloud have no
equal service at all.

### B.8 The health rules

The rules stay in the core, with the same limits as today. They read the observation.

| Rule | Today | Proposed |
|---|---|---|
| Canary after an apply | The probe has under 10 % errors | Each check passes, the primary check with under 10 % errors. Each required component is `ok`. Each version that the source gives is the wanted version: one for each instance, or one for the application with `http-json` |
| Incident | Over 20 % errors in 3 samples, when no change applies and no incident is open | The same for the primary check, **or** a required component is `down` or `degraded` in 3 samples. The two conditions of today stay, so a rollout opens no incident |
| Not reachable | — | A required component is `unknown` in 3 samples: an incident with the title "not reachable". A primary check that is `unknown` opens no incident for errors |
| Recovery | 20 of 20 requests succeed | The primary check passes, and each required component is `ok` |

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
| `health.*` | `checks`, `version` |
| — | `volumes` |
| `chart`, `build` (required) | `deploy: {"kind": "helm", "chart": …}` or `deploy: {"kind": "layer"}`, where the layer of the pack does all of the deployment. `build` is optional with both |
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
"version": {"source": "http-json", "path": "/api/version"},
"diagnostics": [
  {"read": "state"},
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
"version": {"source": "file", "component": "app", "path": "ledger/VERSION"},
"volumes": [{"name": "app-data", "component": "app", "path": "ledger/data"}],
"diagnostics": [
  {"read": "state"},
  {"read": "logs", "component": "app", "lines": 60},
  {"read": "logs", "component": "proxy", "lines": 20},
  {"read": "saturation"}
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
| Runner | Started for each job | The site runner in the data centre |
| Primary check | `GET /api/inventory`, from the core | `GET /login` through the proxy, from the site runner |
| Other checks | `GET /api/healthz` | The port of the service on each host; the error lines in its journal; the export of the night |
| Running version | `GET /api/version` | The file `VERSION` on both hosts |
| State | Pods ready of pods wanted | Active on 2 of 2 hosts; the last run of the timer |
| Logs and events | Pod logs, namespace events | The journal of the service; the log file of the proxy for this site |
| Volumes | The volume claims | The file system of the data directory |
| Read credential | A token for `view` in the namespace | An SSH certificate for the helper |
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
| Where do the jobs run? | In a site runner with outbound HTTPS only (B.7) |
| Where do the checks run? | In the site runner, and on the hosts (B.5) |
| What credential does the broker give? | An SSH certificate. No identity service of a cloud is needed (B.7) |
| Who installs the trust on the hosts? | An administrator, one time, at setup (B.7) |
| What is the fix for a full disk? | A finding for a person. A disk does not grow from a variable (B.6) |
| What does the core do when the site is not reachable? | The states are `unknown`. An incident "not reachable" opens, and no incident for errors (B.8) |

Three things must exist before a pack can deploy on such servers, and this design does
not define them:

- A fifth target in `platform/CONTRACT.md` for hosts that exist, with its own conformance
  suite. The contract of today assumes a cloud or a cluster in its state backends, its
  identities and each of its six conformance checks.
- An application layer that the pack brings (`docs/PLAN.md` section 5.7, "Not built").
- A source for packages inside a site whose hosts have no outbound path.

### B.13 Decisions for the owner

| Decision | Recommended | Alternative |
|---|---|---|
| How a pack brings collection | Declared: a closed list of runtimes and check kinds in the core (B.3, way A) | Read scripts in the pack, with a review at each import |
| Access to a host | An SSH certificate from the broker and the host kit (B.7, option 1). It is the only option that works in a data centre | AWS Systems Manager for hosts on AWS, with the certificate for the rest |
| The runner of a site | A runner that stays, is enrolled one time and asks the core for jobs (B.7) | A runner that an administrator starts for each job |
| Who installs the host kit on hosts that exist | An administrator, at `clanker setup` | The first deployment, with a credential that the administrator gives for that one job |
| Runtimes after Kubernetes | `systemd` first, then `docker` for containers on a host with no orchestrator | Only `systemd`. Windows services are not in this design |
| Health limits | Fixed in the core (B.8). A pack sets only what one check is | A pack sets the limits of the rules inside a range that the core gives |
| The version source `layer` | Permitted, and shown on the approval page and in each canary | Refused: each pack must have a version that the core can read |
| What the Investigator reads | The observation: state from the core, logs as scrubbed text (B.6) | The output of the commands, as today |
| Schema change | Schema 2 replaces schema 1, for new environments (B.10) | A migration of an environment that runs |
| Hardware of a server (disks, the RAID, power, the management controller) | Not in this design. No detector uses it, and it belongs to the platform | A collector for it, as a part of the target for hosts that exist |
| Data from a monitoring system that the application has already | Not in this design | A collector that reads a metrics service |
| Names of the fields | The names of this document. No public convention has this record: OpenTelemetry has names for the identity of a service, a host and a pod, and no values for health. The health check format of the IETF is a draft that expired in 2022 | The OpenTelemetry names for the identity fields, when the record goes to another system |

### B.14 Build order

Each step is one pull request or a few, and each leaves Paperboat working.

1. **Components and the observation, on Kubernetes only.** Schema 2 with `components`,
   `incident`, `diagnostics`, `checks` of the kind `http`, the `http-json` version and
   `deploy` of the kind `helm`. The output `targets` in the layer `40-platform`, and the
   rights of the identities from it.
   The `kubernetes` collector writes the observation in the debug session. The job type
   carries reads. The Investigator, the eval cases, the checks of the eval suite and the
   recorded answer of the Investigator change to components. The evals run again.
2. **The `observe` job, on Kubernetes.** State and volumes with the identity `observer`.
   The health history by name. The storage detector reads real samples.
3. **The rules of B.8**, the `tcp` check, the `image` version, the state `unknown` for a
   check.
4. **The site runner.** Enrolment, the jobs of a site, checks from the site.
5. **Access to a host.** The decision of B.13, then the certificate in the broker, the
   host kit, and a test machine that runs systemd.
6. **The `systemd` collector**, checks `from` `host`, `log-match`, `job`, the `file`
   version, and a second example pack on that machine. This step needs the target for
   hosts that exist and the layer of that pack (section B.12).
7. **The `docker` runtime.**

### B.15 Not in this design

- Several packs in one environment, also two packs on one host, and a new version of a
  pack for an environment that runs (`docs/PLAN.md` section 5.7, "Not built").
- The scenes of the simulator, which are about Paperboat.
- Traces, and metrics of the application other than the checks: the depth of a queue, the
  heap of a Java process, a pool of connections.
- The state of a load balancer that is hardware, and of its members.
- Jobs of plain cron with no unit. A pack needs a timer.
- An application server that starts its own managed servers, for example with a node
  manager.
- The hardware of a server, the installation of its operating system, and the end date of
  a certificate.
- Windows hosts.
- The nodes of the platform in the topology view for a target with no cluster.
