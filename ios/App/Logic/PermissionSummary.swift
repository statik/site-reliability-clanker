import Foundation

/// The permission that an approval releases, in plain language. One entry for each layer.
struct PermissionLine: Identifiable, Equatable, Sendable {
  let layer: String
  let identity: String
  let sentence: String
  /// The rule of the broker for this identity, quoted from `/access`.
  let brokerRule: String

  var id: String { layer }
}

struct PermissionSummary: Equatable, Sendable {
  var lines: [PermissionLine] = []
  /// How long a grant lasts, measured on earlier apply grants. Nil when none exists yet.
  var lifetime: String?

  init(detail: WorkItemDetail, changes: ChangeSummary, access: AccessReport?) {
    lifetime = Self.lifetime(of: access?.history ?? detail.grants)
    let duration = lifetime.map { "for at most \($0)" } ?? "for a short, fixed time"
    let item = detail.workItem
    for layer in changes.layers {
      if let plan = layer.plan, !plan.facts.hasChanges { continue }
      let identity = "applier-\(layer.layer)"
      let binding =
        layer.plan.map { "the plan with hash \($0.sha256.shortHash)…" }
        ?? "a plan made at apply time that may only create resources"
      let sentence =
        "The identity \(identity) may apply layer \(layer.layer) in \(item.env), only for "
        + "commit \(item.commit.shortHash)… and \(binding), \(duration). The credential is "
        + "revoked when the job ends."
      let rule = access?.identities.first { $0.name == identity }?.elevation ?? ""
      lines.append(
        PermissionLine(
          layer: layer.layer, identity: identity, sentence: sentence, brokerRule: rule))
    }
  }

  /// The lifetime of the most recent apply grant, for example "30 minutes".
  static func lifetime(of grants: [Grant]) -> String? {
    let applyGrants = grants.filter { $0.kind == "apply" }
    guard let latest = applyGrants.max(by: { $0.issuedAt < $1.issuedAt }) else { return nil }
    return Timestamps.span(from: latest.notBefore, to: latest.expires)
  }
}
