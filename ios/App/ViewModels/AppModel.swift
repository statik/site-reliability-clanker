import Foundation
import Observation

enum AppTab: String, Hashable, Sendable, CaseIterable {
  case home, approvals, activity, ask, access
}

/// A screen that a tab can push.
enum Route: Hashable, Sendable {
  case approval(String)
  case activity(String)
  case jobLog(Int)
  case chat(String)
}

enum StreamStatus: Equatable, Sendable {
  case connecting
  case connected
  case disconnected(reason: String)

  var label: String {
    switch self {
    case .connecting: "Connecting"
    case .connected: "Live"
    case .disconnected: "Offline, retrying"
    }
  }

  var symbol: String {
    switch self {
    case .connecting: "antenna.radiowaves.left.and.right.slash"
    case .connected: "dot.radiowaves.left.and.right"
    case .disconnected: "wifi.exclamationmark"
    }
  }
}

/// State that every screen shares: settings, the demo user, navigation and the live events.
@MainActor
@Observable
final class AppModel {
  private(set) var settings: AppSettings
  private(set) var users: [User] = []
  private(set) var usersError: String?
  /// Changes when the addresses or the user change. Screens reload when it changes.
  private(set) var generation = 0
  private(set) var streamStatus = StreamStatus.connecting
  private(set) var streamProblem: String?
  /// The most recent audit events from the stream, oldest first. Screens read it with an
  /// `EventCursor`, so a burst of events never hides one of them.
  private(set) var recentEvents: [AuditEvent] = []
  static let eventBufferSize = 300
  /// The time of the control plane minus the time of this phone. The control plane of the
  /// prototype runs on a simulated clock.
  private(set) var clockOffset: TimeInterval?

  var selectedTab = AppTab.home
  var paths: [AppTab: [Route]] = [:]
  var showSettings = false

  let options: LaunchOptions
  private let defaults: UserDefaults
  private var streamTask: Task<Void, Never>?

  init(defaults: UserDefaults = .standard, options: LaunchOptions = LaunchOptions()) {
    self.defaults = defaults
    self.options = options
    var settings = AppSettings.load(from: defaults)
    if !options.user.isEmpty { settings.user = options.user }
    self.settings = settings
    applyLaunchNavigation()
  }

  var latestEvent: AuditEvent? { recentEvents.last }

  var currentUser: User? { users.first { $0.name == settings.user } }

  var userLabel: String { currentUser?.display ?? settings.user }

  func controlPlane() throws -> ControlPlaneAPI {
    let url = try AppSettings.baseURL(settings.controlPlane)
    return ControlPlaneAPI(client: APIClient(baseURL: url, user: settings.user))
  }

  func paperboat() throws -> PaperboatAPI {
    let url = try AppSettings.baseURL(settings.paperboat)
    return PaperboatAPI(client: APIClient(baseURL: url, user: settings.user))
  }

  /// The time of the control plane now, or nil before the first answer from it.
  var serverNow: Date? { clockOffset.map { Date().addingTimeInterval($0) } }

  func noteServerTime(_ text: String) {
    guard let date = Timestamps.parse(text) else { return }
    clockOffset = date.timeIntervalSinceNow
  }

  /// Loads the users and starts the event stream. Does nothing while unit tests host the app.
  func start() async {
    guard !options.isUnderTest else { return }
    restartStream()
    await loadUsers()
  }

  func loadUsers() async {
    do {
      users = try await controlPlane().users()
      usersError = nil
    } catch {
      usersError = userMessage(error)
    }
  }

  func update(_ newSettings: AppSettings) {
    guard newSettings != settings else { return }
    settings = newSettings
    settings.save(to: defaults)
    generation += 1
    restartStream()
    Task { await loadUsers() }
  }

  func selectUser(_ name: String) {
    var changed = settings
    changed.user = name
    update(changed)
  }

  // MARK: Navigation

  func path(for tab: AppTab) -> [Route] { paths[tab] ?? [] }

  func push(_ route: Route) {
    paths[selectedTab, default: []].append(route)
  }

  func open(_ citation: Citation) {
    switch citation.type {
    case "work_item": push(.activity(citation.id))
    case "grant": selectedTab = .access
    default: break
    }
  }

  private func applyLaunchNavigation() {
    if options.initialScreen == "settings" {
      showSettings = true
    } else if let tab = AppTab(rawValue: options.initialScreen) {
      selectedTab = tab
    }
    if options.job > 0 {
      selectedTab = .activity
      paths[.activity] = [.jobLog(options.job)]
    } else if !options.workItem.isEmpty {
      switch selectedTab {
      case .activity: paths[.activity] = [.activity(options.workItem)]
      case .ask: paths[.ask] = [.chat(options.workItem)]
      case .home, .approvals, .access:
        selectedTab = .approvals
        paths[.approvals] = [.approval(options.workItem)]
      }
    }
  }

  // MARK: Event stream

  private func restartStream() {
    streamTask?.cancel()
    streamStatus = .connecting
    streamProblem = nil
    let api: ControlPlaneAPI
    do {
      api = try controlPlane()
    } catch {
      streamStatus = .disconnected(reason: userMessage(error))
      return
    }
    streamTask = Task { [weak self] in
      for await update in EventStreamReader(api: api).updates() {
        self?.handle(update)
      }
    }
  }

  func receive(_ event: AuditEvent) {
    recentEvents.append(event)
    if recentEvents.count > Self.eventBufferSize {
      recentEvents.removeFirst(recentEvents.count - Self.eventBufferSize)
    }
  }

  private func handle(_ update: StreamUpdate) {
    switch update {
    case .connected:
      streamStatus = .connected
    case .event(let event):
      receive(event)
    case .disconnected(let reason):
      streamStatus = .disconnected(reason: reason)
    case .unreadable(let detail):
      streamProblem = "The event stream sent a message this app cannot read: \(detail)"
    }
  }
}
