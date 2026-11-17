import Foundation

/// Where the apply of one layer stands, read from the audit events.
enum LayerStatus: Sendable, Equatable {
  case waiting, grantIssued, applying, applied, failed

  var label: String {
    switch self {
    case .waiting: "Waiting"
    case .grantIssued: "Permission granted"
    case .applying: "Applying"
    case .applied: "Applied"
    case .failed: "Failed"
    }
  }

  var symbol: String {
    switch self {
    case .waiting: "circle.dotted"
    case .grantIssued: "key.fill"
    case .applying: "gearshape.2.fill"
    case .applied: "checkmark.circle.fill"
    case .failed: "xmark.octagon.fill"
    }
  }

  var tone: Tone {
    switch self {
    case .waiting: .neutral
    case .grantIssued, .applying: .active
    case .applied: .good
    case .failed: .bad
    }
  }
}

/// The state of the short-lived permission of one layer.
enum GrantStatus: Sendable, Equatable {
  case none
  case issued(id: String, expires: String)
  case revoked(id: String)
  case revokeFailed(id: String)

  var text: String {
    switch self {
    case .none: "No permission issued yet"
    case .issued(let id, let expires):
      "Grant \(id) is active until \(Timestamps.display(expires))"
    case .revoked(let id): "Grant \(id) ended; the credential is gone"
    case .revokeFailed(let id): "Grant \(id) could NOT be revoked"
    }
  }

  var symbol: String {
    switch self {
    case .none: "key.slash"
    case .issued: "key.fill"
    case .revoked: "lock.fill"
    case .revokeFailed: "exclamationmark.lock.fill"
    }
  }
}

struct LayerProgress: Identifiable, Equatable, Sendable {
  let layer: String
  var status = LayerStatus.waiting
  var grant = GrantStatus.none
  var note = ""
  var jobID: Int?

  var id: String { layer }
}

enum VerificationResult: Sendable, Equatable {
  case pending
  case passed(checks: [String])
  case failed(reason: String, checks: [String])
}

/// One row of the run log: a state change, a grant, an apply step or the verification.
struct ProgressStep: Identifiable, Equatable, Sendable {
  let id: Int
  let time: String
  let symbol: String
  let tone: Tone
  let title: String
  let detail: String
}

/// The progress of the latest apply of a work item. It is a pure function of the audit
/// events, so the same code shows a live apply and one that finished weeks ago.
struct ApplyProgress: Equatable, Sendable {
  var layers: [LayerProgress] = []
  var verification = VerificationResult.pending
  var steps: [ProgressStep] = []
  /// Set when the runner refused the apply and the work item went back for approval.
  var refusal = ""
  var hasStarted = false

  private static let stepTypes: Set<String> = [
    "approval.recorded", "workitem.state_changed", "grant.issued", "grant.revoked",
    "grant.revoke_failed", "grant.expired", "apply.started", "apply.succeeded", "apply.failed",
    "apply.refused", "apply.error", "plan.within_envelope", "pr.merged", "verify.passed",
    "verify.failed", "rollback.authorized", "rollback.failed",
  ]

  init(workItem: WorkItem, events: [AuditEvent]) {
    let ordered = events.sorted { $0.seq < $1.seq }
    let start = ordered.lastIndex { Self.startsRun($0) }
    guard let start else {
      layers = workItem.layers.map { LayerProgress(layer: $0) }
      return
    }
    hasStarted = true
    // The decision that released the apply comes just before the state change to Applying.
    let releaseIndex = ordered[..<start].lastIndex { $0.type == "approval.recorded" } ?? start
    let run = Array(ordered[releaseIndex...])
    var byLayer: [String: LayerProgress] = [:]
    for event in run where Self.stepTypes.contains(event.type) {
      Self.apply(event, to: &byLayer)
      note(event)
      steps.append(Self.step(for: event))
    }
    var order = workItem.layers
    for layer in byLayer.keys.sorted() where !order.contains(layer) {
      order.append(layer)
    }
    layers = order.map { byLayer[$0] ?? LayerProgress(layer: $0) }
  }

  private static func startsRun(_ event: AuditEvent) -> Bool {
    event.type == "workitem.state_changed" && event.string("to") == "Applying"
  }

  private mutating func note(_ event: AuditEvent) {
    switch event.type {
    case "verify.passed":
      verification = .passed(checks: event.data["checks"]?.stringArray ?? [])
    case "verify.failed":
      verification = .failed(
        reason: event.string("reason"), checks: event.data["checks"]?.stringArray ?? [])
    case "apply.refused":
      refusal = event.string("reason")
    case "apply.error":
      refusal = event.summary
    default:
      break
    }
  }

  private static func apply(_ event: AuditEvent, to layers: inout [String: LayerProgress]) {
    guard !event.layer.isEmpty else { return }
    var progress = layers[event.layer] ?? LayerProgress(layer: event.layer)
    let grantID = event.string("grant")
    switch event.type {
    case "grant.issued":
      progress.status = .grantIssued
      progress.grant = .issued(id: grantID, expires: event.string("expires"))
    case "apply.started":
      progress.status = .applying
      progress.note = event.string("credential")
    case "grant.revoked", "grant.expired":
      progress.grant = .revoked(id: grantID)
    case "grant.revoke_failed":
      if case .issued(let id, _) = progress.grant { progress.grant = .revokeFailed(id: id) }
    case "apply.succeeded":
      progress.status = .applied
      progress.jobID = event.data["job"]?.intValue
    case "apply.failed":
      progress.status = .failed
      progress.note = event.string("error")
      progress.jobID = event.data["job"]?.intValue
    default:
      break
    }
    layers[event.layer] = progress
  }

  static func step(for event: AuditEvent) -> ProgressStep {
    let look = appearance(of: event)
    return ProgressStep(
      id: event.seq, time: event.ts, symbol: look.symbol, tone: look.tone,
      title: title(of: event), detail: detail(of: event))
  }

  private static func title(of event: AuditEvent) -> String {
    switch event.type {
    case "workitem.state_changed":
      let from = WorkState(name: event.string("from")).label
      let to = WorkState(name: event.string("to")).label
      return "\(from) → \(to)"
    case "approval.recorded":
      return "Decision by \(event.actor): \(event.string("decision").replacing("_", with: " "))"
    default:
      return event.summary
    }
  }

  private static func detail(of event: AuditEvent) -> String {
    switch event.type {
    case "workitem.state_changed": event.string("reason")
    case "approval.recorded": event.string("comment")
    case "apply.started": event.string("credential")
    case "apply.failed": event.string("error")
    case "verify.passed", "verify.failed":
      (event.data["checks"]?.stringArray ?? []).joined(separator: "\n")
    default: ""
    }
  }

  private static func appearance(of event: AuditEvent) -> (symbol: String, tone: Tone) {
    switch event.type {
    case "workitem.state_changed":
      let state = WorkState(name: event.string("to"))
      return (state.symbol, state.tone)
    case "approval.recorded": return ("person.fill.checkmark", .active)
    case "grant.issued": return ("key.fill", .active)
    case "grant.revoked", "grant.expired": return ("lock.fill", .neutral)
    case "grant.revoke_failed": return ("exclamationmark.lock.fill", .bad)
    case "apply.started": return ("gearshape.2.fill", .active)
    case "apply.succeeded", "verify.passed": return ("checkmark.circle.fill", .good)
    case "plan.within_envelope": return ("checkmark.shield", .good)
    case "pr.merged": return ("arrow.triangle.merge", .good)
    case "rollback.authorized": return ("arrow.counterclockwise.circle.fill", .attention)
    case "apply.failed", "apply.refused", "apply.error", "verify.failed", "rollback.failed":
      return ("xmark.octagon.fill", .bad)
    default: return ("circle", .neutral)
    }
  }
}
