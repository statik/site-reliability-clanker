import Foundation
import Testing

@testable import SRC

@Suite("Sign-in of the app")
@MainActor
struct AppSignInTests {
  private func remove(_ suite: String) {
    UserDefaults.standard.removePersistentDomain(forName: suite)
  }

  /// Requests to the control plane API, without the event stream that starts on its own.
  private func apiRequests(_ host: String) -> [StubProtocol.Recorded] {
    StubProtocol.requests(to: host).filter { $0.path != "/api/v1/events/stream" }
  }

  private func streamRequest(_ host: String) async throws -> StubProtocol.Recorded {
    for _ in 0..<200 {
      let found = StubProtocol.requests(to: host).first { $0.path == "/api/v1/events/stream" }
      if let found { return found }
      try await Task.sleep(for: .milliseconds(10))
    }
    throw APIError.transport(url: host, detail: "The app did not open the event stream.")
  }

  @Test func aServerWithoutSignInGetsTheDemoUserHeader() async throws {
    let (app, host, _, suite) = try SignInStub.app()
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", .json(#"{"sign_in": false}"#))
    StubProtocol.answer(
      host: host, path: "/api/v1/users", StubProtocol.Response(body: try Fixture.data("users")))
    await app.connect()
    #expect(app.signIn == .demo)
    #expect(!app.usesToken)
    #expect(app.currentUser?.name == "dave")
    #expect(app.userLabel == "Dave Lindqvist")
    #expect(app.userName == "dave")
    let stream = try await streamRequest(host)
    let sent = apiRequests(host) + [stream]
    #expect(sent.map(\.path).prefix(2) == ["/api/v1/auth", "/api/v1/users"])
    #expect(sent.allSatisfy { $0.headers[APIClient.userHeader] == "dave" })
    #expect(sent.allSatisfy { $0.headers["Authorization"] == nil })
  }

  @Test func aDemoServerGetsTheUserHeaderEvenWhenATokenIsKeptForItsAddress() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "old-token")
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", .json(#"{"sign_in": false}"#))
    let before = app.generation
    await app.connect()
    #expect(app.signIn == .demo)
    #expect(!app.usesToken)
    #expect(app.generation == before + 1)
    #expect(try app.controlPlane().client.credential == .demoUser("dave"))
    #expect(store.all == [SignInStub.key(host): "old-token"])
  }

  @Test func aServerThatRefusesAnAppWithoutATokenAsksForASignInWithoutANotice() async throws {
    let (app, host, _, suite) = try SignInStub.app()
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", SignInStub.notSignedIn)
    await app.connect()
    #expect(app.signIn == .required(notice: nil))
    #expect(app.userLabel == "Not signed in")
    #expect(app.currentUser == nil)
    try await Task.sleep(for: .milliseconds(100))
    #expect(StubProtocol.requests(to: host).map(\.path) == ["/api/v1/auth"])
  }

  @Test func aKeptTokenSignsTheAppInAndGoesOnEveryRequest() async throws {
    let (app, host, _, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    await app.connect()
    #expect(app.signIn == .signedIn)
    #expect(app.usesToken)
    #expect(app.me?.email == "alice@example.com")
    #expect(app.currentUser?.role == "approver")
    #expect(app.userLabel == "Alice Ng")
    #expect(app.userName == "alice")
    _ = try? await app.controlPlane().workItems()
    let stream = try await streamRequest(host)
    let sent = apiRequests(host) + [stream]
    #expect(
      sent.map(\.path).prefix(4)
        == ["/api/v1/auth", "/api/v1/me", "/api/v1/users", "/api/v1/work-items"])
    #expect(sent.allSatisfy { $0.headers["Authorization"] == "Bearer token-1" })
    #expect(sent.allSatisfy { $0.headers[APIClient.userHeader] == nil })
  }

  @Test func paperboatNeverGetsTheToken() async throws {
    let (app, host, _, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    var settings = app.settings
    settings.paperboat = "http://\(host)"
    app.update(settings)
    _ = try? await app.paperboat().stats()
    let sent = try #require(StubProtocol.requests(to: host).first { $0.path == "/api/stats" })
    #expect(sent.headers["Authorization"] == nil)
  }

  @Test func aTokenThatEndedBeforeTheStartIsDroppedWithAMessage() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "ended")
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", SignInStub.notSignedIn)
    await app.connect()
    guard case .required(let notice) = app.signIn else {
      Issue.record("Expected a sign-in, got \(app.signIn)")
      return
    }
    #expect(notice?.contains("no longer accepts your sign-in") == true)
    #expect(notice?.contains("Sign in again") == true)
    #expect(!app.usesToken)
    #expect(store.all.isEmpty)
  }

  @Test func a401LaterDropsTheTokenAndGoesBackToTheSignIn() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    await app.connect()
    #expect(app.signIn == .signedIn)
    StubProtocol.answer(host: host, path: "/api/v1/work-items", SignInStub.notSignedIn)
    await #expect(throws: APIError.server(status: 401, message: "not signed in")) {
      _ = try await app.controlPlane().workItems()
    }
    guard case .required(let notice) = app.signIn else {
      Issue.record("Expected a sign-in, got \(app.signIn)")
      return
    }
    #expect(notice?.contains("no longer accepts your sign-in") == true)
    #expect(app.me == nil)
    #expect(store.all.isEmpty)
    #expect(try app.controlPlane().client.credential == .demoUser("dave"))
  }

  @Test func a403DoesNotEndTheSignIn() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    await app.connect()
    StubProtocol.answer(
      host: host, path: "/api/v1/work-items", .json(#"{"error": "no"}"#, status: 403))
    _ = try? await app.controlPlane().workItems()
    #expect(app.signIn == .signedIn)
    #expect(store.all == [SignInStub.key(host): "token-1"])
  }

  @Test func a401ForATokenThatTheAppNoLongerUsesChangesNothing() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "old")
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    let early = try app.controlPlane()
    await app.completeSignIn(token: "new")
    StubProtocol.answer(host: host, path: "/api/v1/work-items", SignInStub.notSignedIn)
    _ = try? await early.workItems()
    #expect(app.signIn == .signedIn)
    #expect(store.all == [SignInStub.key(host): "new"])
  }

  @Test func aServerThatDoesNotAnswerLeavesTheAppAsItWas() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", .failure(.cannotConnectToHost))
    await app.connect()
    #expect(app.signIn == .unknown)
    #expect(app.usesToken)
    #expect(store.all == [SignInStub.key(host): "token-1"])
    #expect(app.userLabel == "Signed in")
  }

  @Test func signOutForgetsTheTokenAndSaysSo() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "token-1")
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    await app.connect()
    app.signOut()
    #expect(app.signIn == .required(notice: "You signed out. This phone no longer has the token."))
    #expect(store.all.isEmpty)
    #expect(app.me == nil)
    #expect(!app.usesToken)
  }

  @Test func eachServerHasItsOwnToken() async throws {
    let (app, host, store, suite) = try SignInStub.app(token: "token-a")
    defer { remove(suite) }
    let other = try StubProtocol.newHost().host
    try store.save("token-b", for: SignInStub.key(other))
    let third = try StubProtocol.newHost().host
    #expect(try app.controlPlane().client.credential == .bearer("token-a"))
    var settings = app.settings
    settings.controlPlane = "http://\(other)"
    app.update(settings)
    #expect(try app.controlPlane().client.credential == .bearer("token-b"))
    settings.controlPlane = "http://\(third)"
    app.update(settings)
    #expect(try app.controlPlane().client.credential == .demoUser("dave"))
    #expect(store.all == [SignInStub.key(host): "token-a", SignInStub.key(other): "token-b"])
  }

  @Test func theTokenIsNotInUserDefaults() async throws {
    let (app, host, _, suite) = try SignInStub.app()
    defer { remove(suite) }
    try SignInStub.signedInAnswers(host: host)
    await app.completeSignIn(token: "secret-token-value")
    let defaults = try #require(UserDefaults(suiteName: suite))
    let kept = defaults.persistentDomain(forName: suite) ?? [:]
    #expect(!kept.isEmpty)
    #expect(!"\(kept)".contains("secret-token-value"))
  }

  @Test func aKeychainFailureIsShownAndTheAppStaysSignedIn() async throws {
    let host = try StubProtocol.newHost().host
    let suite = "sign-in-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    defer { remove(suite) }
    var settings = AppSettings()
    settings.controlPlane = "http://\(host)"
    settings.save(to: defaults)
    let locked = MemoryTokenStore(failure: KeychainError(operation: "save", status: -25308))
    let app = AppModel(defaults: defaults, session: StubProtocol.session(), tokens: locked)
    #expect(app.keychainProblem?.contains("status -25308") == true)
    try SignInStub.signedInAnswers(host: host)
    await app.completeSignIn(token: "token-1")
    #expect(app.signIn == .signedIn)
    #expect(app.keychainProblem?.contains("until the app stops") == true)
  }

  @Test func theSignInScreenRunsTheFlowAndSignsTheAppIn() async throws {
    let (app, host, store, suite) = try SignInStub.app()
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/api/v1/auth", SignInStub.notSignedIn)
    await app.connect()
    #expect(app.signIn == .required(notice: nil))
    StubProtocol.answer(host: host, path: "/auth/device/code", SignInStub.grant())
    StubProtocol.answer(
      host: host, path: "/auth/device/token", SignInStub.refusal("authorization_pending"),
      SignInStub.token)
    try SignInStub.signedInAnswers(host: host)
    let before = app.generation
    let model = SignInModel(sleep: { _ in })
    await model.run(app: app)
    #expect(model.step == .idle)
    #expect(app.signIn == .signedIn)
    #expect(app.userLabel == "Alice Ng")
    #expect(app.generation > before)
    #expect(store.all == [SignInStub.key(host): "token-1"])
    let me = try #require(StubProtocol.requests(to: host).first { $0.path == "/api/v1/me" })
    #expect(me.headers["Authorization"] == "Bearer token-1")
  }

  @Test func theSignInScreenShowsWhyAFlowEnded() async throws {
    let (app, host, store, suite) = try SignInStub.app()
    defer { remove(suite) }
    StubProtocol.answer(host: host, path: "/auth/device/code", SignInStub.grant())
    StubProtocol.answer(host: host, path: "/auth/device/token", SignInStub.refusal("access_denied"))
    let model = SignInModel(sleep: { _ in })
    await model.run(app: app)
    #expect(model.step == .failed(message: userMessage(SignInError.denied)))
    #expect(!app.usesToken)
    #expect(store.all.isEmpty)
    StubProtocol.answer(host: host, path: "/auth/device/code", .json("<html>", status: 200))
    await model.run(app: app)
    guard case .failed(let message) = model.step else {
      Issue.record("Expected a failure, got \(model.step)")
      return
    }
    #expect(message.contains("gave no sign-in code"))
  }
}

@Suite("Sign-in of requests")
struct RequestSignInTests {
  private final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0
    var value: Int { lock.withLock { count } }
    func add() { lock.withLock { count += 1 } }
  }

  @Test func aBearerClientSendsTheTokenAndNoUserHeader() async throws {
    let (client, host) = try StubProtocol.client(credential: .bearer("token-1"), body: "[]")
    _ = try await ControlPlaneAPI(client: client).workItems()
    let sent = try #require(StubProtocol.requests(to: host).first)
    #expect(sent.headers["Authorization"] == "Bearer token-1")
    #expect(sent.headers[APIClient.userHeader] == nil)
  }

  @Test func onlyA401RunsTheSignInHook() async throws {
    let counter = Counter()
    var (client, host) = try StubProtocol.client(
      credential: .bearer("token-1"), status: 401, body: #"{"error": "not signed in"}"#)
    client.onUnauthorized = { counter.add() }
    await #expect(throws: APIError.server(status: 401, message: "not signed in")) {
      _ = try await ControlPlaneAPI(client: client).overview()
    }
    #expect(counter.value == 1)
    StubProtocol.answer(
      host: host, path: "/api/v1/overview", .json(#"{"error": "no"}"#, status: 403))
    _ = try? await ControlPlaneAPI(client: client).overview()
    StubProtocol.answer(host: host, path: "/api/v1/overview", .json("{}", status: 500))
    _ = try? await ControlPlaneAPI(client: client).overview()
    #expect(counter.value == 1)
  }

  @Test func a401OnTheEventStreamRunsTheSignInHook() async throws {
    let counter = Counter()
    var (client, host) = try StubProtocol.client(
      credential: .bearer("token-1"), status: 401, body: #"{"error": "not signed in"}"#)
    client.onUnauthorized = { counter.add() }
    var reader = EventStreamReader(api: ControlPlaneAPI(client: client))
    reader.retryDelay = .seconds(60)
    var first: StreamUpdate?
    for await update in reader.updates() {
      first = update
      break
    }
    guard case .disconnected = first else {
      Issue.record("Expected a disconnect, got \(String(describing: first))")
      return
    }
    #expect(counter.value == 1)
    let sent = try #require(StubProtocol.requests(to: host).first)
    #expect(sent.path == "/api/v1/events/stream")
    #expect(sent.headers["Authorization"] == "Bearer token-1")
  }

  @Test func theSignedInPersonIsReadWithTheEmail() throws {
    let me = try Fixture.decode(User.self, "me")
    #expect(me == User(name: "alice", display: "Alice Ng", role: "approver",
      email: "alice@example.com"))
    #expect(try Fixture.decode(AuthMode.self, json: #"{"sign_in": true}"#).signIn)
  }
}
