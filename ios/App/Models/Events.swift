import Foundation

/// One entry of the hash-chained audit log. `ts` is the virtual time of the control plane.
struct AuditEvent: Decodable, Sendable, Equatable, Identifiable {
  var seq = 0
  var ts = ""
  var wallTS = ""
  var type = ""
  var actor = ""
  var env = ""
  var layer = ""
  var workItem = ""
  var summary = ""
  var data: [String: JSONValue] = [:]

  var id: Int { seq }

  enum CodingKeys: String, CodingKey {
    case seq, ts, type, actor, env, layer, summary, data
    case wallTS = "wall_ts"
    case workItem = "work_item"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    seq = try c.decode(Int.self, forKey: .seq)
    ts = try c.text(.ts)
    wallTS = try c.text(.wallTS)
    type = try c.decode(String.self, forKey: .type)
    actor = try c.text(.actor)
    env = try c.text(.env)
    layer = try c.text(.layer)
    workItem = try c.text(.workItem)
    summary = try c.text(.summary)
    data = try c.map(.data)
  }

  func string(_ key: String) -> String { data[key]?.stringValue ?? "" }
}

/// A signed record that the broker exchanges for a short-lived credential.
struct Grant: Decodable, Sendable, Equatable, Identifiable {
  var id = ""
  var workItem = ""
  var env = ""
  var layer = ""
  var identity = ""
  var kind = ""
  var commit = ""
  var planSHA256 = ""
  var tier = ""
  var approvers: [String] = []
  var notBefore = ""
  var expires = ""
  var status = ""
  var issuedAt = ""
  var endedAt = ""

  enum CodingKeys: String, CodingKey {
    case id, env, layer, identity, kind, commit, tier, approvers, expires, status
    case workItem = "work_item"
    case planSHA256 = "plan_sha256"
    case notBefore = "not_before"
    case issuedAt = "issued_at"
    case endedAt = "ended_at"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    workItem = try c.text(.workItem)
    env = try c.text(.env)
    layer = try c.text(.layer)
    identity = try c.text(.identity)
    kind = try c.text(.kind)
    commit = try c.text(.commit)
    planSHA256 = try c.text(.planSHA256)
    tier = try c.text(.tier)
    approvers = try c.list(.approvers)
    notBefore = try c.text(.notBefore)
    expires = try c.text(.expires)
    status = try c.text(.status)
    issuedAt = try c.text(.issuedAt)
    endedAt = try c.text(.endedAt)
  }

  var isActive: Bool { status == "issued" || status == "active" }
}

/// An identity of the agent and what it may do with and without a grant.
struct AccessIdentity: Decodable, Sendable, Equatable, Identifiable {
  var name = ""
  var standing = ""
  var elevation = ""

  var id: String { name }

  enum CodingKeys: String, CodingKey { case name, standing, elevation }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    name = try c.text(.name)
    standing = try c.text(.standing)
    elevation = try c.text(.elevation)
  }
}

struct AccessReport: Decodable, Sendable {
  var identities: [AccessIdentity] = []
  var active: [Grant] = []
  var history: [Grant] = []

  enum CodingKeys: String, CodingKey { case identities, active, history }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    identities = try c.list(.identities)
    active = try c.list(.active)
    history = try c.list(.history)
  }
}

struct Citation: Decodable, Sendable, Equatable, Hashable {
  var type = ""
  var id = ""
  var label = ""

  enum CodingKeys: String, CodingKey { case type, id, label }

  init(type: String, id: String, label: String) {
    self.type = type
    self.id = id
    self.label = label
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    type = try c.text(.type)
    id = try c.text(.id)
    label = try c.text(.label)
  }
}

/// One message of the Ask conversation. `role` is "user" or "assistant".
struct ChatEntry: Decodable, Sendable, Equatable, Identifiable {
  var id = 0
  var workItem = ""
  var user = ""
  var role = ""
  var body = ""
  var citations: [Citation] = []
  var at = ""

  enum CodingKeys: String, CodingKey {
    case id, user, role, body, citations, at
    case workItem = "work_item"
  }

  init() {}

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.integer(.id)
    workItem = try c.text(.workItem)
    user = try c.text(.user)
    role = try c.text(.role)
    body = try c.text(.body)
    citations = try c.list(.citations)
    at = try c.text(.at)
  }

  var isAssistant: Bool { role == "assistant" }
}
