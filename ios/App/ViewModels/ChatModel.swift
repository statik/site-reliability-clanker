import Foundation
import Observation

@MainActor
@Observable
final class ChatModel {
  /// The work item that scopes the conversation, or "" for the global conversation.
  let workItem: String
  private(set) var entries: [ChatEntry] = []
  private(set) var error: String?
  private(set) var isSending = false
  private(set) var hasLoaded = false
  var draft = ""

  init(workItem: String) {
    self.workItem = workItem
  }

  static let globalSuggestions = [
    "What is waiting on me and what happens if I leave it a week?",
    "Are we behind on any patches? Which ones matter?",
    "What changed in the last 30 days, and who approved each change?",
    "When was the last successful restore test?",
    "What does the agent have access to right now?",
  ]

  static let scopedSuggestions = [
    "What happens if I leave this for a week?",
    "What is the worst thing that can go wrong with this change?",
    "How do we undo this change?",
  ]

  var suggestions: [String] {
    workItem.isEmpty ? Self.globalSuggestions : Self.scopedSuggestions
  }

  var canSend: Bool {
    !isSending && !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
  }

  func load(app: AppModel) async {
    do {
      entries = try await app.controlPlane().chat(workItem: workItem)
      error = nil
    } catch {
      self.error = userMessage(error)
    }
    hasLoaded = true
  }

  func send(app: AppModel) async {
    let question = draft.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !question.isEmpty, !isSending else { return }
    isSending = true
    defer { isSending = false }
    do {
      let api = try app.controlPlane()
      _ = try await api.ask(workItem: workItem, question: question)
      draft = ""
      entries = try await api.chat(workItem: workItem)
      error = nil
    } catch {
      self.error = userMessage(error)
    }
  }
}

@MainActor
@Observable
final class AccessModel {
  private(set) var report: AccessReport?
  private(set) var error: String?
  private var cursor = EventCursor()

  /// Reads the new stream events. Returns true when one of them is about a grant.
  func consume(_ buffer: [AuditEvent]) -> Bool {
    cursor.take(from: buffer).contains { $0.type.hasPrefix("grant.") }
  }

  func load(app: AppModel) async {
    do {
      report = try await app.controlPlane().access()
      error = nil
    } catch {
      self.error = userMessage(error)
    }
  }

  /// The newest grants first, at most `limit` of them.
  nonisolated static func recent(_ grants: [Grant], limit: Int) -> [Grant] {
    Array(grants.sorted { $0.issuedAt > $1.issuedAt }.prefix(limit))
  }
}

@MainActor
@Observable
final class JobLogModel {
  private(set) var jobLog: JobLog?
  private(set) var error: String?

  func load(id: Int, app: AppModel) async {
    do {
      jobLog = try await app.controlPlane().jobLog(id)
      error = nil
    } catch {
      self.error = userMessage(error)
    }
  }
}
