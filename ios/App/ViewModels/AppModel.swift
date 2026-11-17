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

/// What the app knows about the sign-in of the control plane.
enum SignInStatus: Equatable, Sendable {
  /// No answer yet. The app works as in the demo until the control plane says otherwise.
  case unknown
  /// The control plane has no sign-in: the app names a demo user in a header.
  case demo
  /// The control plane refuses the app until a person signs in. `notice` says why an
  /// earlier sign-in is gone, when there was one.
  case required(notice: String?)
  case signedIn
}

/// State that every screen shares: settings, the user, navigation and the live events.
@MainActor
@Observable
final class AppModel {
  private(set) var settings: AppSettings
  private(set) var users: [User] = []
  private(set) var usersError: String?
  private(set) var signIn = SignInStatus.unknown
  /// The person behind the token, from `GET /api/v1/me`. Nil in the demo.
  private(set) var me: User?
  /// Set when the Keychain could not read, save or remove the token.
  private(set) var keychainProblem: String?
  /// The token of the sign-in for the control plane in `settings`. It is a secret: it
  /// lives here and in the Keychain, and never in UserDefaults or in a log.
  private var token: String?
  /// Changes when the addresses, the user or the sign-in change. Screens reload when it changes.
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
  private let session: URLSession
  private let tokens: any TokenStore
  private var streamTask: Task<Void, Never>?

  init(
    defaults: UserDefaults = .standard, options: LaunchOptions = LaunchOptions(),
    session: URLSession = .shared, tokens: any TokenStore = KeychainTokenStore()
  ) {
    self.defaults = defaults
    self.options = options
    self.session = session
    self.tokens = tokens
    var settings = AppSettings.load(from: defaults)
    if !options.user.isEmpty { settings.user = options.user }
    self.settings = settings
    loadToken()
    applyLaunchNavigation()
  }

  var latestEvent: AuditEvent? { recentEvents.last }

  /// Whether requests to the control plane carry the token of a sign-in.
  var usesToken: Bool { token != nil }

  /// The user that the control plane sees behind the requests of the app.
  var currentUser: User? {
    usesToken ? me : users.first { $0.name == settings.user }
  }

  /// The name of that user in the records of the control plane.
  var userName: String { usesToken ? me?.name ?? "" : settings.user }

  var userLabel: String {
    if case .required = signIn { return "Not signed in" }
    if usesToken { return me?.display ?? "Signed in" }
    return currentUser?.display ?? settings.user
  }

  /// A client for the control plane. It sends the token when the app has one, and the
  /// name of the demo user when not. A 401 answer ends the sign-in.
  func controlPlane() throws -> ControlPlaneAPI {
    let url = try AppSettings.baseURL(settings.controlPlane)
    let sent = token
    var client = APIClient(
      baseURL: url, credential: sent.map(Credential.bearer) ?? .demoUser(settings.user),
      session: session)
    client.onUnauthorized = { [weak self] in await self?.sessionEnded(sent: sent) }
    return ControlPlaneAPI(client: client)
  }

  /// A client for Paperboat. It never gets the token: the token is for the control plane.
  func paperboat() throws -> PaperboatAPI {
    let url = try AppSettings.baseURL(settings.paperboat)
    return PaperboatAPI(
      client: APIClient(baseURL: url, credential: .demoUser(settings.user), session: session))
  }

  func deviceSignIn() throws -> DeviceSignIn {
    let url = try AppSettings.baseURL(settings.controlPlane)
    return DeviceSignIn(client: APIClient(baseURL: url, credential: .anonymous, session: session))
  }

  /// The time of the control plane now, or nil before the first answer from it.
  var serverNow: Date? { clockOffset.map { Date().addingTimeInterval($0) } }

  func noteServerTime(_ text: String) {
    guard let date = Timestamps.parse(text) else { return }
    clockOffset = date.timeIntervalSinceNow
  }

  /// Connects to the control plane. Does nothing while unit tests host the app.
  func start() async {
    guard !options.isUnderTest else { return }
    await connect()
  }

  /// Asks the control plane whether people sign in, then loads the users and starts the
  /// event stream, unless a person must sign in first.
  func connect() async {
    streamTask?.cancel()
    me = nil
    signIn = .unknown
    let asked = settings.controlPlane
    do {
      let api = try controlPlane()
      let mode = try await api.auth()
      let person = mode.signIn ? try await api.me() : nil
      guard asked == settings.controlPlane else { return }
      me = person
      signIn = mode.signIn ? .signedIn : .demo
      if !mode.signIn, usesToken {
        // A demo reads only the user header, so a token kept for this address is of no use.
        token = nil
        generation += 1
      }
    } catch {
      // A 401 answer already ran `sessionEnded`. After every other failure the status
      // stays unknown, and each screen shows the failure of its own request.
      guard asked == settings.controlPlane else { return }
    }
    if case .required = signIn { return }
    restartStream()
    await loadUsers()
  }

  /// Keeps the token of a finished device flow and connects with it.
  func completeSignIn(token: String) async {
    if let serverKey {
      do {
        try tokens.save(token, for: serverKey)
        keychainProblem = nil
      } catch {
        keychainProblem = userMessage(error) + " You stay signed in until the app stops."
      }
    }
    self.token = token
    generation += 1
    await connect()
  }

  /// Forgets the token on this phone. The control plane has no way to end a token early.
  func signOut() {
    guard usesToken else { return }
    forgetToken()
    endSession(notice: "You signed out. This phone no longer has the token.")
  }

  /// Runs when the control plane answered 401 to a request that carried `sent`, or the
  /// demo header when `sent` is nil. An answer to a credential that the app no longer
  /// uses changes nothing.
  func sessionEnded(sent: String?) {
    guard sent == token else { return }
    if case .required = signIn { return }
    guard usesToken else {
      endSession(notice: nil)
      return
    }
    forgetToken()
    endSession(
      notice: "The control plane no longer accepts your sign-in. A sign-in lasts twelve "
        + "hours. Sign in again.")
  }

  private func endSession(notice: String?) {
    streamTask?.cancel()
    me = nil
    signIn = .required(notice: notice)
  }

  private var serverKey: String? {
    (try? AppSettings.baseURL(settings.controlPlane)).map(AppSettings.serverKey)
  }

  private func loadToken() {
    token = nil
    keychainProblem = nil
    guard let serverKey else { return }
    do {
      token = try tokens.token(for: serverKey)
    } catch {
      keychainProblem = userMessage(error)
    }
  }

  private func forgetToken() {
    token = nil
    guard let serverKey else { return }
    do {
      try tokens.remove(for: serverKey)
      keychainProblem = nil
    } catch {
      keychainProblem = userMessage(error)
    }
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
    let serverChanged = newSettings.controlPlane != settings.controlPlane
    settings = newSettings
    settings.save(to: defaults)
    generation += 1
    if serverChanged {
      loadToken()
      Task { await connect() }
      return
    }
    if case .required = signIn { return }
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
        guard let self else { return }
        self.handle(update)
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
