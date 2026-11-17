import Foundation

/// "X of N approvers", computed the way the control plane computes it: an approval counts
/// only when it covers the current commit and the current plan hash of every planned layer.
///
/// While a work item waits for approval the plan hashes must match exactly, as on the server.
/// After the apply started, layers that were deferred have a plan too. An approval then still
/// counts when every plan it names is unchanged; the later plans were inside its envelope.
struct ApproverProgress: Equatable, Sendable {
  /// Distinct approvers whose approval covers the current commit and plans, oldest first.
  var valid: [Approval] = []
  /// Approvals of an older commit or plan. They no longer count.
  var stale: [Approval] = []
  /// Decisions that are not approvals: reject and request changes.
  var other: [Approval] = []
  var required = 0

  var count: Int { valid.count }
  var isComplete: Bool { required > 0 && count >= required }
  var remaining: Int { max(required - count, 0) }

  var headline: String {
    if required == 0 { return "No approval is needed" }
    return "\(count) of \(required) \(required == 1 ? "approver" : "approvers")"
  }

  init(detail: WorkItemDetail) {
    required = detail.approversRequired
    var currentHashes: [String: String] = [:]
    for plan in detail.currentPlans {
      currentHashes[plan.layer] = plan.sha256
    }
    let exact = detail.workItem.state == .awaitingApproval
    var seen: Set<String> = []
    for approval in detail.approvals {
      guard approval.decision == "approve" else {
        other.append(approval)
        continue
      }
      let samePlans =
        exact
        ? approval.planHashes == currentHashes
        : approval.planHashes.allSatisfy { currentHashes[$0.key] == $0.value }
      let covers = approval.commit == detail.workItem.commit && samePlans
      if covers && !seen.contains(approval.approver) {
        seen.insert(approval.approver)
        valid.append(approval)
      } else if !covers {
        stale.append(approval)
      }
    }
  }

  /// True when this user already gave an approval that still counts.
  func hasValidApproval(from user: String) -> Bool {
    valid.contains { $0.approver == user }
  }
}
