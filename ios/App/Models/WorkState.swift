import Foundation

/// How a status reads on screen: always an icon and a word, with colour as a third signal.
enum Tone: Sendable, Equatable, CaseIterable {
  case good, attention, bad, active, neutral
}

/// The states of the work-item lifecycle. A state this app does not know stays readable.
enum WorkState: Sendable, Equatable, Hashable {
  case detected, drafting, planned, awaitingApproval, changesRequested, approved, scheduled
  case applying, verifying, done, failed, rolledBack, rejected
  case unknown(String)

  static let known: [(name: String, state: WorkState)] = [
    ("Detected", .detected), ("Drafting", .drafting), ("Planned", .planned),
    ("AwaitingApproval", .awaitingApproval), ("ChangesRequested", .changesRequested),
    ("Approved", .approved), ("Scheduled", .scheduled), ("Applying", .applying),
    ("Verifying", .verifying), ("Done", .done), ("Failed", .failed),
    ("RolledBack", .rolledBack), ("Rejected", .rejected),
  ]

  init(name: String) {
    self = Self.known.first { $0.name == name }?.state ?? .unknown(name)
  }

  var label: String {
    switch self {
    case .detected: "Detected"
    case .drafting: "Drafting"
    case .planned: "Planned"
    case .awaitingApproval: "Needs decision"
    case .changesRequested: "Changes requested"
    case .approved: "Approved"
    case .scheduled: "Scheduled"
    case .applying: "Applying"
    case .verifying: "Verifying"
    case .done: "Done"
    case .failed: "Failed"
    case .rolledBack: "Rolled back"
    case .rejected: "Rejected"
    case .unknown(let name): name.isEmpty ? "Unknown" : name
    }
  }

  var symbol: String {
    switch self {
    case .detected: "dot.radiowaves.left.and.right"
    case .drafting: "pencil.line"
    case .planned: "list.bullet.clipboard"
    case .awaitingApproval: "hand.raised.fill"
    case .changesRequested: "arrow.uturn.backward.circle"
    case .approved: "checkmark.seal"
    case .scheduled: "calendar.badge.clock"
    case .applying: "gearshape.2.fill"
    case .verifying: "stethoscope"
    case .done: "checkmark.circle.fill"
    case .failed: "xmark.octagon.fill"
    case .rolledBack: "arrow.counterclockwise.circle.fill"
    case .rejected: "nosign"
    case .unknown: "questionmark.circle"
    }
  }

  var tone: Tone {
    switch self {
    case .awaitingApproval, .changesRequested, .rolledBack: .attention
    case .approved, .scheduled, .applying, .verifying: .active
    case .done: .good
    case .failed: .bad
    case .detected, .drafting, .planned, .rejected, .unknown: .neutral
    }
  }

  /// A work item in one of these states does not change again.
  var isFinal: Bool {
    switch self {
    case .done, .failed, .rolledBack, .rejected: true
    case .detected, .drafting, .planned, .awaitingApproval, .changesRequested, .approved,
      .scheduled, .applying, .verifying, .unknown:
      false
    }
  }

  /// The apply was released and has not reached a final state.
  var isRunning: Bool {
    switch self {
    case .approved, .applying, .verifying: true
    case .detected, .drafting, .planned, .awaitingApproval, .changesRequested, .scheduled,
      .done, .failed, .rolledBack, .rejected, .unknown:
      false
    }
  }
}

/// Risk tiers from section 6.3 of the plan. Policy code sets the tier, never the model.
enum RiskTier: Sendable, Equatable, Hashable {
  case t0, t1, t2, t3
  case none
  case unknown(String)

  init(name: String) {
    switch name {
    case "T0": self = .t0
    case "T1": self = .t1
    case "T2": self = .t2
    case "T3": self = .t3
    case "": self = .none
    default: self = .unknown(name)
    }
  }

  var code: String {
    switch self {
    case .t0: "T0"
    case .t1: "T1"
    case .t2: "T2"
    case .t3: "T3"
    case .none: "No tier"
    case .unknown(let name): name
    }
  }

  var meaning: String {
    switch self {
    case .t0: "Read-only"
    case .t1: "Routine and reversible"
    case .t2: "Normal change"
    case .t3: "High risk"
    case .none: "Not classified"
    case .unknown: "Unknown tier"
    }
  }

  var approvalRule: String {
    switch self {
    case .t0: "Needs no approval."
    case .t1: "Covered by a standing approval that a human signed once. Each use is logged."
    case .t2: "Needs one approver."
    case .t3: "Needs one or two approvers, as the control plane sets, and a maintenance window."
    case .none: "Policy has not classified this work item."
    case .unknown: "This app does not know the approval rule of this tier."
    }
  }

  var symbol: String {
    switch self {
    case .t0: "eye"
    case .t1: "arrow.triangle.2.circlepath"
    case .t2: "wrench.and.screwdriver"
    case .t3: "exclamationmark.triangle.fill"
    case .none, .unknown: "questionmark.diamond"
    }
  }
}
