import Foundation
import Observation

/// What Paperboat itself reports: version, health and the business counters.
struct BusinessStatus: Sendable {
  var stats: PaperboatStats
  var version: PaperboatVersion
  var health: PaperboatHealth
}

@MainActor
@Observable
final class HomeModel {
  private(set) var overview: Overview?
  private(set) var business: BusinessStatus?
  private(set) var healthHistory: [String: [HealthSample]] = [:]
  private(set) var controlPlaneError: String?
  private(set) var businessError: String?
  private(set) var isLoading = false

  private var cursor = EventCursor()

  /// Reads the new stream events. Returns true when one of them changes what Home shows.
  func consume(_ buffer: [AuditEvent]) -> Bool {
    cursor.take(from: buffer).contains(where: Self.affectsHome)
  }

  func load(app: AppModel) async {
    isLoading = true
    defer { isLoading = false }
    async let business: Void = loadBusiness(app: app)
    await loadOverview(app: app)
    await business
  }

  private func loadOverview(app: AppModel) async {
    do {
      let api = try app.controlPlane()
      let loaded = try await api.overview()
      overview = loaded
      controlPlaneError = nil
      app.noteServerTime(loaded.now)
      for environment in loaded.environments {
        healthHistory[environment.name] = try await api.health(env: environment.name, limit: 60)
      }
    } catch {
      controlPlaneError = userMessage(error)
    }
  }

  private func loadBusiness(app: AppModel) async {
    do {
      let api = try app.paperboat()
      async let stats = api.stats()
      async let version = api.version()
      async let health = api.health()
      business = try await BusinessStatus(stats: stats, version: version, health: health)
      businessError = nil
    } catch {
      business = nil
      businessError = userMessage(error)
    }
  }

  /// True when a stream event can change what Home shows.
  nonisolated static func affectsHome(_ event: AuditEvent) -> Bool {
    event.type == "workitem.state_changed" || event.type == "workitem.created"
      || event.type.hasPrefix("incident.")
  }
}
