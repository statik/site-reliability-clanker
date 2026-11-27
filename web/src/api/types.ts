export type Role = "viewer" | "approver" | "admin";

export interface User {
  name: string;
  display: string;
  role: Role;
  email: string;
  last_seen: string;
  /** The person gave the name; no identity provider confirmed it. */
  self_declared: boolean;
}

export interface Explanation {
  sentence: string;
  why: string;
  downtime: string;
  user_effect: string;
  cost_change: string;
  duration: string;
  pre_checks: string[] | null;
  backup: string;
  rollback: string;
  verification: string;
  if_deferred: string;
  source: string;
}

export interface WorkItem {
  id: string;
  title: string;
  kind: string;
  env: string;
  layers: string[] | null;
  state: string;
  tier: string;
  tier_rule: string;
  trigger: string;
  pr_id: number;
  commit: string;
  base_commit: string;
  summary: string;
  explanation: Explanation;
  scheduled_for: string;
  /** The start of the maintenance window that a scheduled work item waits for, or "". */
  maintenance_window: string;
  rollback_of: string;
  created_at: string;
  updated_at: string;
}

export interface ResourceChange {
  address: string;
  type: string;
  name: string;
  action: string;
  details: string[] | null;
}

export interface PlanFacts {
  add: number;
  change: number;
  destroy: number;
  replace: number;
  resources: ResourceChange[] | null;
  outputs: string[] | null;
  /** Set only when the core warns the approver, for example of a version that goes down
   *  across a release that changed the database schema. */
  warnings?: string[];
}

export interface PolicyDecision {
  tier: string;
  rule: string;
  reasons: string[] | null;
  create_only: boolean;
}

export interface Plan {
  id: number;
  work_item: string;
  layer: string;
  commit: string;
  plan_sha256: string;
  facts: PlanFacts;
  policy: PolicyDecision;
  superseded: boolean;
  created_at: string;
}

export interface Approval {
  id: number;
  work_item: string;
  approver: string;
  decision: string;
  comment: string;
  commit: string;
  plan_hashes: Record<string, string> | null;
  at: string;
}

export interface Job {
  id: number;
  work_item: string;
  layer: string;
  kind: string;
  status: string;
  grant_id: string;
  tool_digest: string;
  started_at: string;
  ended_at: string;
}

export interface AuditEvent {
  seq: number;
  ts: string;
  wall_ts: string;
  type: string;
  actor: string;
  env?: string;
  layer?: string;
  work_item?: string;
  summary: string;
  data: Record<string, unknown> | null;
  prev_hash: string;
  hash: string;
}

export interface Grant {
  id: string;
  work_item: string;
  env: string;
  layer: string;
  identity: string;
  kind: string;
  commit: string;
  plan_sha256: string;
  tier: string;
  approvers: string[] | null;
  not_before: string;
  expires: string;
  signature: string;
  status: string;
  issued_at: string;
  ended_at: string;
}

export interface PullRequest {
  id: number;
  repo: string;
  title: string;
  body: string;
  branch: string;
  base: string;
  head_commit: string;
  base_commit: string;
  state: string;
  author: string;
  work_item: string;
  created_at: string;
  merged_at: string;
}

export interface WorkItemDetail {
  work_item: WorkItem;
  plans: Plan[] | null;
  approvals: Approval[] | null;
  jobs: Job[] | null;
  events: AuditEvent[] | null;
  approvers_required: number;
  deferred_layers: string[] | null;
  pull?: PullRequest;
  diff?: string;
  grants: Grant[] | null;
}

export interface HealthSample {
  ts: string;
  env: string;
  ok: number;
  total: number;
  p95_ms: number;
  version: string;
}

export interface EnvironmentSummary {
  name: string;
  cloud: string;
  layers: Record<string, boolean>;
  versions: Record<string, string>;
  health: HealthSample | null;
  app_url: string;
  open_work_items: number;
  deployed: boolean;
  snapshot_ts: string;
}

export interface CatchUp {
  since: string;
  applied: number;
  failed: number;
  rolled_back: number;
  incidents: number;
  waiting: number;
  narrative: string;
  narrative_source: string;
}

export interface ChainVerification {
  ok: boolean;
  count: number;
  head_hash: string;
  broken_seq?: number;
  reason?: string;
}

export interface Overview {
  now: string;
  as_of: string;
  environments: EnvironmentSummary[];
  state_counts: Record<string, number>;
  waiting: WorkItem[];
  catch_up: CatchUp;
  chain: ChainVerification;
  agent_mode: string;
  planned_clouds: string[];
}

/** The local inference: the model server on the machine of the control plane. */
export interface LocalInference {
  /** False when this control plane cannot start or stop a model server. */
  available: boolean;
  reason?: string;
  url?: string;
  running: boolean;
  version?: string;
  model?: string;
  model_present: boolean;
  model_loaded: boolean;
  started_by_src: boolean;
}

/** The language model of the control plane. The API never sends a key. */
export interface ModelSettings {
  mode: string;
  provider: string;
  models: Record<string, string> | null;
  base_url?: string;
  /** The AWS region of the mode bedrock. */
  region?: string;
  key_set: boolean;
  local: LocalInference;
  /** What a start or a stop did. */
  done?: string;
}

export interface TimelineMarker {
  seq: number;
  ts: string;
  env: string;
  layer: string;
  type: string;
  category: string;
  outcome: string;
  work_item: string;
  summary: string;
  tier: string | null;
  kind: string | null;
  actor: string;
}

export interface FutureItem {
  ts: string;
  env: string;
  kind: "scheduled" | "forecast" | "window";
  title: string;
  work_item?: string;
  tier?: string;
  advisory?: string;
  /** Where a forecast comes from when it has no advisory, for example "Volume samples". */
  source?: string;
}

export interface HealthBucket {
  ts: string;
  end: string;
  attainment: number;
  requests: number;
}

export interface TimelineLane {
  env: string;
  cloud: string;
  layers: string[];
}

export interface Timeline {
  now: string;
  from: string;
  to: string;
  lanes: TimelineLane[];
  markers: TimelineMarker[];
  future: FutureItem[];
  health: Record<string, HealthBucket[]>;
}

/** The live state of a component, of an instance or of a node of the topology. */
export type ComponentState = "ok" | "degraded" | "down" | "unknown";

export interface ObservedInstance {
  id: string;
  state: ComponentState;
  version?: string;
  restarts: number;
}

export interface ObservedComponent {
  name: string;
  runtime: string;
  state: ComponentState;
  wanted: number;
  ready: number;
  instances: ObservedInstance[];
}

/** The last report of the observer of one environment. */
export interface Observation {
  schema: number;
  env: string;
  at: string;
  application: string;
  version: { wanted: string; source: string };
  components: ObservedComponent[];
  checks: unknown[];
  volumes: unknown[];
  /** The age of the report, or -1 when no report arrived yet. */
  age_seconds: number;
  /** True when no report arrived for 60 seconds. Every state is then `unknown`. */
  stale: boolean;
}

export interface TopologyNode {
  id: string;
  layer: string;
  kind: string;
  label: string;
  detail: string;
  version: string;
  status: ComponentState;
  badges: string[];
}

export interface TopologyEdge {
  from: string;
  to: string;
}

export interface Topology {
  env: string;
  as_of: string;
  snapshot_ts: string;
  snapshot_reason: string;
  layers: string[];
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  health: HealthSample | null;
}

export interface PostureCell {
  current: string;
  latest: string;
  behind: number;
  status: string;
}

export interface Posture {
  components: string[];
  rows: { env: string; cells: Record<string, PostureCell> }[];
  as_of: string;
}

export interface Identity {
  name: string;
  standing: string;
  elevation: string;
}

export interface Access {
  identities: Identity[];
  active: Grant[];
  history: Grant[];
}

export interface Finding {
  id: number;
  detector: string;
  env: string;
  key: string;
  title: string;
  detail: Record<string, unknown> | null;
  state: string;
  work_item: string;
  at: string;
}

export interface NotificationChannel {
  channel: string;
  configured: boolean;
  targets: string[] | null;
}

export interface NotificationRoute {
  name: string;
  types?: string[];
  tiers?: string[];
  envs?: string[];
  channels: string[];
  audience: string;
  urgency: string;
}

export interface Delivery {
  id: number;
  event_seq: number;
  channel: string;
  target: string;
  status: string;
  attempts: number;
  error: string;
  subject: string;
  at: string;
}

export interface Notifications {
  channels: NotificationChannel[];
  rules: { routes: NotificationRoute[] };
  deliveries: Delivery[];
  rules_source: string;
}

export interface Repo {
  name: string;
  head: string;
  branches: string[];
  clone_url: string;
}

export interface TreeEntry {
  name: string;
  path: string;
  type: string;
}

export interface Blob {
  path: string;
  ref: string;
  content: string;
}

export interface Commit {
  commit: string;
  author: string;
  date: string;
  subject: string;
}

export interface Citation {
  type: string;
  id: string;
  label: string;
}

export interface ChatEntry {
  id: number;
  work_item: string;
  user: string;
  role: "user" | "assistant";
  body: string;
  citations: Citation[] | null;
  at: string;
}

export interface AgentCall {
  id: number;
  role: string;
  work_item: string;
  model: string;
  mode: string;
}

export interface Scene {
  name: string;
  title: string;
  detail: string;
  use_case: string;
  done: boolean;
}

export interface SimState {
  now: string;
  offset_seconds: number;
  scenes: Scene[];
  next: string;
}

export interface JobLog {
  job: Job;
  log: string;
}

export interface PlanJSON {
  plan_sha256: string;
  plan: Record<string, unknown>;
}

export interface RunbookField {
  type: "string" | "integer";
  description: string;
  values?: string[];
}

/** A runbook of the runbooks repository, as `GET /runbooks` lists it. */
export interface Runbook {
  name: string;
  title?: string;
  description: string;
  tier: string;
  incident_mitigation?: boolean;
  inputs?: Record<string, RunbookField>;
  outputs?: Record<string, RunbookField>;
  /** The user who signed the standing approval, or "" when nobody did. */
  standing_approver: string;
  /** When the standing approval was signed, or "" when the core did not record it. */
  standing_signed_at: string;
}

/** The answer of a signature or a revocation of a standing approval. */
export interface StandingApproval {
  runbook: string;
  signer: string;
  signed_at: string;
}
