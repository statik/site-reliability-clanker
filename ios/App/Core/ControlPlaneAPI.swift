import Foundation

/// A decision of a human on a work item, as `POST /work-items/{id}/decision` expects it.
struct DecisionRequest: Encodable, Sendable, Equatable {
  enum Kind: String, Encodable, Sendable, CaseIterable {
    case approve
    case reject
    case requestChanges = "request_changes"
  }

  let decision: Kind
  let comment: String
  /// ISO 8601 in UTC with milliseconds, or "" to apply at once.
  let scheduledFor: String

  enum CodingKeys: String, CodingKey {
    case decision, comment
    case scheduledFor = "scheduled_for"
  }

  init(decision: Kind, comment: String, scheduledFor: Date? = nil) {
    self.decision = decision
    self.comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
    self.scheduledFor = scheduledFor.map(Timestamps.format) ?? ""
  }
}

struct ChatRequest: Encodable, Sendable {
  let workItem: String
  let question: String

  enum CodingKeys: String, CodingKey {
    case question
    case workItem = "work_item"
  }
}

/// The endpoints of the Clanker control plane that this app uses.
struct ControlPlaneAPI: Sendable {
  let client: APIClient

  func users() async throws -> [User] { try await client.get("/api/v1/users") }

  func overview() async throws -> Overview { try await client.get("/api/v1/overview") }

  func workItems() async throws -> [WorkItem] { try await client.get("/api/v1/work-items") }

  func workItem(_ id: String) async throws -> WorkItemDetail {
    try await client.get("/api/v1/work-items/\(id)")
  }

  func decide(_ id: String, _ decision: DecisionRequest) async throws -> WorkItem {
    try await client.post("/api/v1/work-items/\(id)/decision", body: decision)
  }

  func jobLog(_ id: Int) async throws -> JobLog { try await client.get("/api/v1/jobs/\(id)/log") }

  func access() async throws -> AccessReport { try await client.get("/api/v1/access") }

  func health(env: String, limit: Int) async throws -> [HealthSample] {
    try await client.get(
      "/api/v1/health",
      query: [
        URLQueryItem(name: "env", value: env), URLQueryItem(name: "limit", value: "\(limit)"),
      ])
  }

  func chat(workItem: String) async throws -> [ChatEntry] {
    try await client.get(
      "/api/v1/chat", query: [URLQueryItem(name: "work_item", value: workItem)])
  }

  func ask(workItem: String, question: String) async throws -> ChatEntry {
    try await client.post(
      "/api/v1/chat", body: ChatRequest(workItem: workItem, question: question))
  }

  /// Events with a sequence number above `seq`, oldest first. Fills the gap after a reconnect.
  func events(after seq: Int) async throws -> [AuditEvent] {
    try await client.get(
      "/api/v1/events",
      query: [
        URLQueryItem(name: "after", value: "\(seq)"), URLQueryItem(name: "order", value: "asc"),
      ])
  }
}

/// The endpoints of the Paperboat business application.
struct PaperboatAPI: Sendable {
  let client: APIClient

  func stats() async throws -> PaperboatStats { try await client.get("/api/stats") }
  func version() async throws -> PaperboatVersion { try await client.get("/api/version") }
  func health() async throws -> PaperboatHealth { try await client.get("/api/healthz") }
}
