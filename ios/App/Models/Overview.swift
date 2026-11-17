import Foundation

/// A demo user of the control plane. The prototype has no real sign-in.
struct User: Decodable, Sendable, Equatable, Identifiable, Hashable {
  var name = ""
  var display = ""
  var role = ""

  var id: String { name }

  enum CodingKeys: String, CodingKey { case name, display, role }

  init(name: String, display: String, role: String) {
    self.name = name
    self.display = display
    self.role = role
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    name = try c.decode(String.self, forKey: .name)
    display = try c.text(.display)
    role = try c.text(.role)
  }

  /// The server accepts decisions only from these roles.
  var canDecide: Bool { role == "approver" || role == "admin" }
}

/// One health sample of an environment: `ok` of `total` probe requests succeeded.
struct HealthSample: Decodable, Sendable, Equatable, Identifiable {
  var ts = ""
  var env = ""
  var ok = 0
  var total = 0
  var p95MS = 0.0
  var version = ""

  var id: String { ts }

  enum CodingKeys: String, CodingKey {
    case ts, env, ok, total, version
    case p95MS = "p95_ms"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    ts = try c.text(.ts)
    env = try c.text(.env)
    ok = try c.integer(.ok)
    total = try c.integer(.total)
    p95MS = try c.number(.p95MS)
    version = try c.text(.version)
  }

  var successRate: Double { total == 0 ? 0 : Double(ok) / Double(total) }
}

struct EnvironmentStatus: Decodable, Sendable, Identifiable {
  var name = ""
  var cloud = ""
  var layers: [String: Bool] = [:]
  var versions: [String: String] = [:]
  var health: HealthSample?
  var appURL = ""
  var openWorkItems = 0
  var deployed = false

  var id: String { name }

  enum CodingKeys: String, CodingKey {
    case name, cloud, layers, versions, health, deployed
    case appURL = "app_url"
    case openWorkItems = "open_work_items"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    name = try c.text(.name)
    cloud = try c.text(.cloud)
    layers = try c.map(.layers)
    versions = try c.map(.versions)
    health = try c.decodeIfPresent(HealthSample.self, forKey: .health)
    appURL = try c.text(.appURL)
    openWorkItems = try c.integer(.openWorkItems)
    deployed = try c.flag(.deployed)
  }
}

struct CatchUp: Decodable, Sendable {
  var since = ""
  var applied = 0
  var failed = 0
  var rolledBack = 0
  var incidents = 0
  var waiting = 0
  var narrative = ""
  var narrativeSource = ""

  enum CodingKeys: String, CodingKey {
    case since, applied, failed, incidents, waiting, narrative
    case rolledBack = "rolled_back"
    case narrativeSource = "narrative_source"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    since = try c.text(.since)
    applied = try c.integer(.applied)
    failed = try c.integer(.failed)
    rolledBack = try c.integer(.rolledBack)
    incidents = try c.integer(.incidents)
    waiting = try c.integer(.waiting)
    narrative = try c.text(.narrative)
    narrativeSource = try c.text(.narrativeSource)
  }
}

/// The result of verifying the hash chain of the audit log.
struct ChainStatus: Decodable, Sendable {
  var ok = false
  var count = 0
  var headHash = ""
  var brokenSeq = 0
  var reason = ""

  enum CodingKeys: String, CodingKey {
    case ok, count, reason
    case headHash = "head_hash"
    case brokenSeq = "broken_seq"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    ok = try c.flag(.ok)
    count = try c.integer(.count)
    headHash = try c.text(.headHash)
    brokenSeq = try c.integer(.brokenSeq)
    reason = try c.text(.reason)
  }
}

struct Overview: Decodable, Sendable {
  var now = ""
  var environments: [EnvironmentStatus] = []
  var stateCounts: [String: Int] = [:]
  var waiting: [WorkItem] = []
  var catchUp = CatchUp()
  var chain = ChainStatus()
  var agentMode = ""

  enum CodingKeys: String, CodingKey {
    case now, environments, waiting, chain
    case stateCounts = "state_counts"
    case catchUp = "catch_up"
    case agentMode = "agent_mode"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    now = try c.text(.now)
    environments = try c.list(.environments)
    stateCounts = try c.map(.stateCounts)
    waiting = try c.list(.waiting)
    catchUp = try c.decodeIfPresent(CatchUp.self, forKey: .catchUp) ?? CatchUp()
    chain = try c.decodeIfPresent(ChainStatus.self, forKey: .chain) ?? ChainStatus()
    agentMode = try c.text(.agentMode)
  }
}

/// Business counters from Paperboat's `/api/stats`.
struct PaperboatStats: Decodable, Sendable {
  var requests = 0
  var errors = 0
  var p95MS = 0.0
  var fishAvailable = 0
  var rentalsActive = 0
  var deliveriesOut = 0
  var ticketsOpen = 0

  enum CodingKeys: String, CodingKey {
    case requests, errors
    case p95MS = "p95_ms"
    case fishAvailable = "fish_available"
    case rentalsActive = "rentals_active"
    case deliveriesOut = "deliveries_out"
    case ticketsOpen = "tickets_open"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    requests = try c.integer(.requests)
    errors = try c.integer(.errors)
    p95MS = try c.number(.p95MS)
    fishAvailable = try c.integer(.fishAvailable)
    rentalsActive = try c.integer(.rentalsActive)
    deliveriesOut = try c.integer(.deliveriesOut)
    ticketsOpen = try c.integer(.ticketsOpen)
  }
}

struct PaperboatVersion: Decodable, Sendable {
  var version = ""
  var schemaVersion = 0

  enum CodingKeys: String, CodingKey {
    case version
    case schemaVersion = "schema_version"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    version = try c.text(.version)
    schemaVersion = try c.integer(.schemaVersion)
  }
}

struct PaperboatHealth: Decodable, Sendable {
  var status = ""

  enum CodingKeys: String, CodingKey { case status }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    status = try c.text(.status)
  }

  var isOK: Bool { status == "ok" }
}
