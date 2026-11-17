import Foundation

/// What a plan does to one resource.
enum ChangeAction: Sendable, Equatable {
  case destroy, replace, update, create
  case other(String)

  init(name: String) {
    switch name {
    case "destroy": self = .destroy
    case "replace": self = .replace
    case "update": self = .update
    case "create": self = .create
    default: self = .other(name)
    }
  }

  var label: String {
    switch self {
    case .destroy: "Destroy"
    case .replace: "Replace"
    case .update: "Update in place"
    case .create: "Create"
    case .other(let name): name.isEmpty ? "Change" : name.capitalized
    }
  }

  var symbol: String {
    switch self {
    case .destroy: "trash.fill"
    case .replace: "arrow.triangle.swap"
    case .update: "pencil.circle.fill"
    case .create: "plus.circle.fill"
    case .other: "circle.dashed"
    }
  }

  var tone: Tone {
    switch self {
    case .destroy, .replace: .bad
    case .update: .active
    case .create: .good
    case .other: .neutral
    }
  }

  /// Destroys and replacements lose something. They are listed first.
  var isDestructive: Bool { self == .destroy || self == .replace }

  var sortOrder: Int {
    switch self {
    case .destroy: 0
    case .replace: 1
    case .update: 2
    case .create: 3
    case .other: 4
    }
  }
}

/// One resource of a plan in words a manager can read.
struct ResourceLine: Identifiable, Equatable, Sendable {
  let id: String
  let layer: String
  let action: ChangeAction
  let title: String
  let address: String
  let details: [String]

  init(layer: String, resource: ResourceChange) {
    id = layer + "/" + resource.address
    self.layer = layer
    action = ChangeAction(name: resource.action)
    title = Humanise.resource(type: resource.type, name: resource.name)
    address = resource.address
    details = resource.details.map(Humanise.detail)
  }
}

/// The planned changes of one layer, or the note that the layer is planned later.
struct LayerChanges: Identifiable, Equatable, Sendable {
  let layer: String
  /// Nil when the layer is deferred: it has no saved plan yet.
  let plan: Plan?
  let resources: [ResourceLine]
  /// True when the layer was deferred at approval time and planned during the apply.
  var plannedAtApply = false

  var id: String { layer }
  var isDeferred: Bool { plan == nil }

  var countsText: String {
    guard let facts = plan?.facts else { return "Planned at apply time" }
    if !facts.hasChanges { return "No changes" }
    return Humanise.counts(
      add: facts.add, change: facts.change, destroy: facts.destroy, replace: facts.replace)
  }
}

/// "What changes", grouped by layer, with destroys and replacements called out first.
struct ChangeSummary: Equatable, Sendable {
  var layers: [LayerChanges] = []
  var add = 0
  var change = 0
  var destroy = 0
  var replace = 0

  /// Every resource that is destroyed or replaced, across all layers.
  var destructive: [ResourceLine] {
    layers.flatMap(\.resources).filter(\.action.isDestructive)
  }

  var deferredLayers: [String] { layers.filter(\.isDeferred).map(\.layer) }
  var totalText: String {
    Humanise.counts(add: add, change: change, destroy: destroy, replace: replace)
  }
  var hasPlans: Bool { layers.contains { !$0.isDeferred } }

  init(detail: WorkItemDetail) {
    let plans = detail.currentPlans
    var order = detail.workItem.layers
    for plan in plans where !order.contains(plan.layer) {
      order.append(plan.layer)
    }
    let policy = detail.events.last { $0.type == "policy.evaluated" }
    let deferredAtApproval = policy?.data["deferred_layers"]?.stringArray ?? []
    for layer in order {
      let plan = plans.last { $0.layer == layer }
      if plan == nil && !detail.deferredLayers.contains(layer) { continue }
      let lines = (plan?.facts.resources ?? [])
        .map { ResourceLine(layer: layer, resource: $0) }
        .sorted { $0.action.sortOrder < $1.action.sortOrder }
      layers.append(
        LayerChanges(
          layer: layer, plan: plan, resources: lines,
          plannedAtApply: plan != nil && deferredAtApproval.contains(layer)))
      add += plan?.facts.add ?? 0
      change += plan?.facts.change ?? 0
      destroy += plan?.facts.destroy ?? 0
      replace += plan?.facts.replace ?? 0
    }
  }
}

/// Turns identifiers of the platform into plain words.
enum Humanise {
  private static let layerNames = [
    "00-bootstrap": "Bootstrap", "10-network": "Network", "20-cluster": "Cluster",
    "30-data": "Data", "40-platform": "Platform", "50-app": "Application",
  ]

  private static let acronyms: Set<String> = ["iam", "dns", "ip", "tls", "kms", "vpc", "rwx"]

  /// "50-app" becomes "Application (50-app)". An unknown layer keeps its name.
  static func layer(_ layer: String) -> String {
    guard let name = layerNames[layer] else { return layer }
    return "\(name) (\(layer))"
  }

  /// "helm_release" and "paperboat" become "Helm release “paperboat”".
  static func resource(type: String, name: String) -> String {
    let words = type.split(separator: "_").map(String.init)
    var parts: [String] = []
    for (index, word) in words.enumerated() {
      if acronyms.contains(word) {
        parts.append(word.uppercased())
      } else if index == 0 {
        parts.append(word.prefix(1).uppercased() + word.dropFirst())
      } else {
        parts.append(word)
      }
    }
    let kind = parts.isEmpty ? "Resource" : parts.joined(separator: " ")
    return name.isEmpty ? kind : "\(kind) “\(name)”"
  }

  /// "values.api.replicas: 1 → 2" becomes "values › api › replicas: 1 → 2".
  static func detail(_ detail: String) -> String {
    guard let separator = detail.range(of: ": ") else { return detail }
    let path = detail[..<separator.lowerBound].replacingOccurrences(of: ".", with: " › ")
    return path + detail[separator.lowerBound...]
  }

  /// "1 to change, 2 to destroy". Zero counts are left out.
  static func counts(add: Int, change: Int, destroy: Int, replace: Int) -> String {
    var parts: [String] = []
    if destroy > 0 { parts.append("\(destroy) to destroy") }
    if replace > 0 { parts.append("\(replace) to replace") }
    if change > 0 { parts.append("\(change) to change") }
    if add > 0 { parts.append("\(add) to create") }
    return parts.isEmpty ? "No changes" : parts.joined(separator: ", ")
  }

  /// "runbook:db-backup" becomes "Runbook: db-backup"; "app-upgrade" becomes "App upgrade".
  static func kind(_ kind: String) -> String {
    if kind.isEmpty { return "Work item" }
    let text = kind.replacingOccurrences(of: ":", with: ": ")
      .replacingOccurrences(of: "-", with: " ", options: [], range: nil)
    if kind.hasPrefix("runbook:") {
      return "Runbook: " + kind.dropFirst("runbook:".count)
    }
    return text.prefix(1).uppercased() + text.dropFirst()
  }
}
