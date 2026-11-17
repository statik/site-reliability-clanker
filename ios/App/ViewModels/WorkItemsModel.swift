import Foundation
import Observation

/// The states of the work items in a list, with how many items are in each.
struct StateCount: Identifiable, Equatable, Sendable {
  let state: WorkState
  let name: String
  let count: Int

  var id: String { name }
}

@MainActor
@Observable
final class WorkItemsModel {
  private(set) var items: [WorkItem] = []
  private(set) var error: String?
  private(set) var hasLoaded = false
  /// The raw state name to show, or nil for all states.
  var stateFilter: String?

  private var cursor = EventCursor()

  /// Reads the new stream events. Returns true when one of them changes the list.
  func consume(_ buffer: [AuditEvent]) -> Bool {
    cursor.take(from: buffer).contains(where: Self.affectsList)
  }

  var awaiting: [WorkItem] { Self.awaitingApproval(items) }
  var filtered: [WorkItem] { Self.filter(items, state: stateFilter) }
  var stateCounts: [StateCount] { Self.stateCounts(items) }

  func load(app: AppModel) async {
    do {
      items = try await app.controlPlane().workItems()
      error = nil
    } catch {
      self.error = userMessage(error)
    }
    hasLoaded = true
  }

  nonisolated static func awaitingApproval(_ items: [WorkItem]) -> [WorkItem] {
    items.filter { $0.state == .awaitingApproval }
  }

  nonisolated static func filter(_ items: [WorkItem], state: String?) -> [WorkItem] {
    guard let state else { return items }
    return items.filter { $0.stateName == state }
  }

  /// The states that occur, in lifecycle order. Unknown states come last, by name.
  nonisolated static func stateCounts(_ items: [WorkItem]) -> [StateCount] {
    var counts: [String: Int] = [:]
    for item in items {
      counts[item.stateName, default: 0] += 1
    }
    let order = WorkState.known.map(\.name)
    let names = counts.keys.sorted { left, right in
      let leftIndex = order.firstIndex(of: left) ?? order.count
      let rightIndex = order.firstIndex(of: right) ?? order.count
      return leftIndex == rightIndex ? left < right : leftIndex < rightIndex
    }
    return names.map { StateCount(state: WorkState(name: $0), name: $0, count: counts[$0] ?? 0) }
  }

  nonisolated static func affectsList(_ event: AuditEvent) -> Bool {
    event.type == "workitem.state_changed" || event.type == "workitem.created"
  }
}
