import Foundation

/// The text of the confirmation step: it restates the tier and exactly what will run.
struct DecisionSummary: Equatable, Sendable {
  let headline: String
  /// What the runner does when the decision takes effect, one line for each layer.
  let runs: [String]
  let consequence: String

  init(
    choice: DecisionChoice, detail: WorkItemDetail, changes: ChangeSummary,
    approvers: ApproverProgress, user: String, scheduledFor: Date?
  ) {
    let item = detail.workItem
    let tier = "\(item.riskTier.code) (\(item.riskTier.meaning.lowercased()))"
    switch choice {
    case .approve, .schedule:
      headline = "Approve the tier \(tier) change “\(item.title)” in \(item.env)."
      runs = Self.runs(item: item, changes: changes)
      consequence = Self.approvalConsequence(
        approvers: approvers, user: user, scheduledFor: choice == .schedule ? scheduledFor : nil)
    case .requestChanges:
      headline = "Send the tier \(tier) change “\(item.title)” back for changes."
      runs = []
      consequence = "Nothing runs. The work item moves to Changes requested."
    case .reject:
      headline = "Reject the tier \(tier) change “\(item.title)”."
      runs = []
      consequence =
        item.pullID == 0
        ? "Nothing runs. The work item is closed as Rejected."
        : "Nothing runs. Pull request #\(item.pullID) is closed and the work item is Rejected."
    }
  }

  private static func runs(item: WorkItem, changes: ChangeSummary) -> [String] {
    changes.layers.map { layer in
      guard let plan = layer.plan else {
        return "\(layer.layer): planned at apply time; may only create resources"
      }
      return "\(layer.layer): \(layer.countsText.lowercased()), commit "
        + "\(item.commit.shortHash)…, plan \(plan.sha256.shortHash)…"
    }
  }

  private static func approvalConsequence(
    approvers: ApproverProgress, user: String, scheduledFor: Date?
  ) -> String {
    let already = approvers.hasValidApproval(from: user)
    let countAfter = approvers.count + (already ? 0 : 1)
    if countAfter < approvers.required {
      let missing = approvers.required - countAfter
      let waiting =
        "This is approval \(countAfter) of \(approvers.required). Nothing runs until "
        + "\(missing) more \(missing == 1 ? "approver approves" : "approvers approve")."
      guard scheduledFor != nil else { return waiting }
      return waiting + " The control plane keeps the start time only from the last approver, "
        + "so your start time is NOT kept. Agree the time with the last approver."
    }
    let prefix =
      "There is no separate apply step: with this approval (\(countAfter) of "
      + "\(approvers.required)) the runner applies exactly this commit and plan"
    guard let scheduledFor else { return prefix + ", starting at once." }
    return prefix + ", starting at \(Timestamps.display(scheduledFor))."
  }
}

/// The date range of the schedule picker, on the clock of the control plane.
enum ScheduleWindow {
  /// The earliest time that the app accepts: one minute after the server's now.
  static func earliest(serverNow: Date) -> Date {
    let next = serverNow.addingTimeInterval(60)
    let seconds = next.timeIntervalSinceReferenceDate
    return Date(timeIntervalSinceReferenceDate: (seconds / 60).rounded(.up) * 60)
  }

  /// The time the picker starts with: the next full hour at least 30 minutes away.
  static func suggested(serverNow: Date) -> Date {
    let base = serverNow.addingTimeInterval(30 * 60).timeIntervalSinceReferenceDate
    return Date(timeIntervalSinceReferenceDate: (base / 3600).rounded(.up) * 3600)
  }

  /// A time at or before the server's now would apply at once instead of scheduling.
  static func isValid(_ date: Date, serverNow: Date) -> Bool {
    date > serverNow
  }
}
