import Foundation
import Observation

/// The four choices on the approval page.
enum DecisionChoice: String, Identifiable, CaseIterable, Sendable {
  case approve
  case schedule
  case requestChanges = "request_changes"
  case reject

  var id: String { rawValue }

  var title: String {
    switch self {
    case .approve: "Approve"
    case .schedule: "Approve and schedule"
    case .requestChanges: "Request changes"
    case .reject: "Reject"
    }
  }

  var symbol: String {
    switch self {
    case .approve: "checkmark.seal.fill"
    case .schedule: "calendar.badge.clock"
    case .requestChanges: "arrow.uturn.backward.circle.fill"
    case .reject: "xmark.circle.fill"
    }
  }

  var confirmTitle: String {
    switch self {
    case .approve: "Confirm approval"
    case .schedule: "Confirm approval and schedule"
    case .requestChanges: "Send back for changes"
    case .reject: "Confirm rejection"
    }
  }

  var kind: DecisionRequest.Kind {
    switch self {
    case .approve, .schedule: .approve
    case .requestChanges: .requestChanges
    case .reject: .reject
    }
  }
}

@MainActor
@Observable
final class WorkItemDetailModel {
  let id: String
  private(set) var detail: WorkItemDetail?
  private(set) var access: AccessReport?
  private(set) var error: String?
  private(set) var accessError: String?
  private(set) var decisionError: APIError?
  private(set) var isSubmitting = false
  /// What the server answered to the last decision of this session.
  private(set) var lastDecision: DecisionChoice?

  init(id: String) {
    self.id = id
  }

  var changes: ChangeSummary { detail.map(ChangeSummary.init) ?? ChangeSummary(detail: .init()) }
  var approvers: ApproverProgress {
    detail.map(ApproverProgress.init) ?? ApproverProgress(detail: .init())
  }
  var progress: ApplyProgress {
    ApplyProgress(workItem: detail?.workItem ?? WorkItem(), events: detail?.events ?? [])
  }
  var permission: PermissionSummary {
    PermissionSummary(detail: detail ?? .init(), changes: changes, access: access)
  }

  func load(app: AppModel) async {
    do {
      let api = try app.controlPlane()
      detail = try await api.workItem(id)
      error = nil
      await loadAccess(api: api)
    } catch {
      self.error = userMessage(error)
    }
  }

  private func loadAccess(api: ControlPlaneAPI) async {
    do {
      access = try await api.access()
      accessError = nil
    } catch {
      accessError = userMessage(error)
    }
  }

  private var cursor = EventCursor()

  /// Reads the new stream events and adds those of this work item at once. Returns true
  /// when at least one belongs to this work item, so that the caller reloads the detail.
  func consume(_ buffer: [AuditEvent]) -> Bool {
    var isAffected = false
    for event in cursor.take(from: buffer) where receive(event) {
      isAffected = true
    }
    return isAffected
  }

  /// Adds a stream event of this work item at once, so progress shows without a reload.
  /// Returns true when the event belongs to this work item.
  func receive(_ event: AuditEvent) -> Bool {
    guard event.workItem == id else { return false }
    guard var current = detail else { return true }
    if !current.events.contains(where: { $0.seq == event.seq }) {
      current.events.append(event)
      if event.type == "workitem.state_changed" {
        current.workItem.stateName = event.string("to")
      }
      detail = current
    }
    return true
  }

  func clearDecisionError() {
    decisionError = nil
  }

  /// Sends the decision. Returns true when the server accepted it.
  func submit(
    _ choice: DecisionChoice, comment: String, scheduledFor: Date?, app: AppModel
  ) async -> Bool {
    isSubmitting = true
    defer { isSubmitting = false }
    let request = DecisionRequest(
      decision: choice.kind, comment: comment,
      scheduledFor: choice == .schedule ? scheduledFor : nil)
    do {
      _ = try await app.controlPlane().decide(id, request)
      decisionError = nil
      lastDecision = choice
      await load(app: app)
      return true
    } catch let error as APIError {
      decisionError = error
      return false
    } catch {
      decisionError = .transport(url: app.settings.controlPlane, detail: userMessage(error))
      return false
    }
  }
}
