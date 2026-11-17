import Foundation

/// The prose on the approval page. A model or a template wrote it; `source` says which.
struct Explanation: Decodable, Sendable, Equatable {
  var sentence = ""
  var why = ""
  var downtime = ""
  var userEffect = ""
  var costChange = ""
  var duration = ""
  var preChecks: [String] = []
  var backup = ""
  var rollback = ""
  var verification = ""
  var ifDeferred = ""
  var source = ""

  enum CodingKeys: String, CodingKey {
    case sentence, why, downtime, duration, backup, rollback, verification, source
    case userEffect = "user_effect"
    case costChange = "cost_change"
    case preChecks = "pre_checks"
    case ifDeferred = "if_deferred"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    sentence = try c.text(.sentence)
    why = try c.text(.why)
    downtime = try c.text(.downtime)
    userEffect = try c.text(.userEffect)
    costChange = try c.text(.costChange)
    duration = try c.text(.duration)
    preChecks = try c.list(.preChecks)
    backup = try c.text(.backup)
    rollback = try c.text(.rollback)
    verification = try c.text(.verification)
    ifDeferred = try c.text(.ifDeferred)
    source = try c.text(.source)
  }

  var isEmpty: Bool { sentence.isEmpty && why.isEmpty && source.isEmpty }
}

/// One unit of work that the control plane tracks.
struct WorkItem: Decodable, Sendable, Identifiable, Equatable, Hashable {
  var id = ""
  var title = ""
  var kind = ""
  var env = ""
  var layers: [String] = []
  var stateName = ""
  var tier = ""
  var tierRule = ""
  var trigger = ""
  var pullID = 0
  var commit = ""
  var baseCommit = ""
  var summary = ""
  var explanation = Explanation()
  var scheduledFor = ""
  var rollbackOf = ""
  var createdAt = ""
  var updatedAt = ""

  enum CodingKeys: String, CodingKey {
    case id, title, kind, env, layers, tier, trigger, commit, summary, explanation
    case stateName = "state"
    case tierRule = "tier_rule"
    case pullID = "pr_id"
    case baseCommit = "base_commit"
    case scheduledFor = "scheduled_for"
    case rollbackOf = "rollback_of"
    case createdAt = "created_at"
    case updatedAt = "updated_at"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    title = try c.text(.title)
    kind = try c.text(.kind)
    env = try c.text(.env)
    layers = try c.list(.layers)
    stateName = try c.text(.stateName)
    tier = try c.text(.tier)
    tierRule = try c.text(.tierRule)
    trigger = try c.text(.trigger)
    pullID = try c.integer(.pullID)
    commit = try c.text(.commit)
    baseCommit = try c.text(.baseCommit)
    summary = try c.text(.summary)
    explanation = try c.decodeIfPresent(Explanation.self, forKey: .explanation) ?? Explanation()
    scheduledFor = try c.text(.scheduledFor)
    rollbackOf = try c.text(.rollbackOf)
    createdAt = try c.text(.createdAt)
    updatedAt = try c.text(.updatedAt)
  }

  static func == (lhs: WorkItem, rhs: WorkItem) -> Bool {
    lhs.id == rhs.id && lhs.stateName == rhs.stateName && lhs.updatedAt == rhs.updatedAt
  }

  func hash(into hasher: inout Hasher) {
    hasher.combine(id)
  }

  var state: WorkState { WorkState(name: stateName) }
  var riskTier: RiskTier { RiskTier(name: tier) }
}

/// One changed resource, read from the plan JSON by the runner.
struct ResourceChange: Decodable, Sendable, Equatable, Identifiable {
  var address = ""
  var type = ""
  var name = ""
  var action = ""
  var details: [String] = []

  var id: String { address }

  enum CodingKeys: String, CodingKey { case address, type, name, action, details }

  init(address: String, type: String, name: String, action: String, details: [String] = []) {
    self.address = address
    self.type = type
    self.name = name
    self.action = action
    self.details = details
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    address = try c.text(.address)
    type = try c.text(.type)
    name = try c.text(.name)
    action = try c.text(.action)
    details = try c.list(.details)
  }
}

/// Counts and names from the plan. No model output can change them.
struct PlanFacts: Decodable, Sendable, Equatable {
  var add = 0
  var change = 0
  var destroy = 0
  var replace = 0
  var resources: [ResourceChange] = []
  var outputs: [String] = []

  enum CodingKeys: String, CodingKey { case add, change, destroy, replace, resources, outputs }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    add = try c.integer(.add)
    change = try c.integer(.change)
    destroy = try c.integer(.destroy)
    replace = try c.integer(.replace)
    resources = try c.list(.resources)
    outputs = try c.list(.outputs)
  }

  var hasChanges: Bool { add + change + destroy + replace > 0 }
}

/// The result of the tier policy for one plan.
struct PolicyDecision: Decodable, Sendable, Equatable {
  var tier = ""
  var rule = ""
  var reasons: [String] = []
  var createOnly = false

  enum CodingKeys: String, CodingKey {
    case tier, rule, reasons
    case createOnly = "create_only"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    tier = try c.text(.tier)
    rule = try c.text(.rule)
    reasons = try c.list(.reasons)
    createOnly = try c.flag(.createOnly)
  }
}

/// A saved plan for one layer of a work item.
struct Plan: Decodable, Sendable, Equatable, Identifiable {
  var id = 0
  var workItem = ""
  var layer = ""
  var commit = ""
  var sha256 = ""
  var facts = PlanFacts()
  var policy = PolicyDecision()
  var superseded = false
  var createdAt = ""

  enum CodingKeys: String, CodingKey {
    case id, layer, commit, facts, policy, superseded
    case workItem = "work_item"
    case sha256 = "plan_sha256"
    case createdAt = "created_at"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.integer(.id)
    workItem = try c.text(.workItem)
    layer = try c.text(.layer)
    commit = try c.text(.commit)
    sha256 = try c.text(.sha256)
    facts = try c.decodeIfPresent(PlanFacts.self, forKey: .facts) ?? PlanFacts()
    policy = try c.decodeIfPresent(PolicyDecision.self, forKey: .policy) ?? PolicyDecision()
    superseded = try c.flag(.superseded)
    createdAt = try c.text(.createdAt)
  }
}

/// One decision by a human, bound to a commit and to the plan hashes of that moment.
struct Approval: Decodable, Sendable, Equatable, Identifiable {
  var id = 0
  var workItem = ""
  var approver = ""
  var decision = ""
  var comment = ""
  var commit = ""
  var planHashes: [String: String] = [:]
  var at = ""

  enum CodingKeys: String, CodingKey {
    case id, approver, decision, comment, commit, at
    case workItem = "work_item"
    case planHashes = "plan_hashes"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.integer(.id)
    workItem = try c.text(.workItem)
    approver = try c.text(.approver)
    decision = try c.text(.decision)
    comment = try c.text(.comment)
    commit = try c.text(.commit)
    planHashes = try c.map(.planHashes)
    at = try c.text(.at)
  }
}

/// One run of the runner: a plan, an apply, a runbook or a debug session.
struct Job: Decodable, Sendable, Equatable, Identifiable, Hashable {
  var id = 0
  var workItem = ""
  var layer = ""
  var kind = ""
  var status = ""
  var grantID = ""
  var toolDigest = ""
  var startedAt = ""
  var endedAt = ""

  enum CodingKeys: String, CodingKey {
    case id, layer, kind, status
    case workItem = "work_item"
    case grantID = "grant_id"
    case toolDigest = "tool_digest"
    case startedAt = "started_at"
    case endedAt = "ended_at"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.integer(.id)
    workItem = try c.text(.workItem)
    layer = try c.text(.layer)
    kind = try c.text(.kind)
    status = try c.text(.status)
    grantID = try c.text(.grantID)
    toolDigest = try c.text(.toolDigest)
    startedAt = try c.text(.startedAt)
    endedAt = try c.text(.endedAt)
  }
}

struct JobLog: Decodable, Sendable {
  var job = Job()
  var log = ""

  enum CodingKeys: String, CodingKey { case job, log }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    job = try c.decodeIfPresent(Job.self, forKey: .job) ?? Job()
    log = try c.text(.log)
  }
}

/// The pull request that carries a change.
struct PullRequest: Decodable, Sendable, Equatable {
  var id = 0
  var repo = ""
  var title = ""
  var body = ""
  var branch = ""
  var base = ""
  var headCommit = ""
  var baseCommit = ""
  var state = ""
  var author = ""

  enum CodingKeys: String, CodingKey {
    case id, repo, title, body, branch, base, state, author
    case headCommit = "head_commit"
    case baseCommit = "base_commit"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.integer(.id)
    repo = try c.text(.repo)
    title = try c.text(.title)
    body = try c.text(.body)
    branch = try c.text(.branch)
    base = try c.text(.base)
    headCommit = try c.text(.headCommit)
    baseCommit = try c.text(.baseCommit)
    state = try c.text(.state)
    author = try c.text(.author)
  }
}

/// Everything the control plane knows about one work item.
struct WorkItemDetail: Decodable, Sendable {
  var workItem = WorkItem()
  var plans: [Plan] = []
  var approvals: [Approval] = []
  var jobs: [Job] = []
  var events: [AuditEvent] = []
  var approversRequired = 0
  var deferredLayers: [String] = []
  var pull: PullRequest?
  var diff = ""
  var grants: [Grant] = []

  enum CodingKeys: String, CodingKey {
    case plans, approvals, jobs, events, pull, diff, grants
    case workItem = "work_item"
    case approversRequired = "approvers_required"
    case deferredLayers = "deferred_layers"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    workItem = try c.decode(WorkItem.self, forKey: .workItem)
    plans = try c.list(.plans)
    approvals = try c.list(.approvals)
    jobs = try c.list(.jobs)
    events = try c.list(.events)
    approversRequired = try c.integer(.approversRequired)
    deferredLayers = try c.list(.deferredLayers)
    pull = try c.decodeIfPresent(PullRequest.self, forKey: .pull)
    diff = try c.text(.diff)
    grants = try c.list(.grants)
  }

  /// The plans that the next approval covers. The API also returns superseded plans.
  var currentPlans: [Plan] { plans.filter { !$0.superseded } }
}
