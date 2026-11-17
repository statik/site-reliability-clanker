import Foundation
import Testing

@testable import SRC

/// A token store in memory. `failure` makes every operation fail, like a locked Keychain.
final class MemoryTokenStore: TokenStore, @unchecked Sendable {
  private let lock = NSLock()
  private var tokens: [String: String]
  private let failure: KeychainError?

  init(_ tokens: [String: String] = [:], failure: KeychainError? = nil) {
    self.tokens = tokens
    self.failure = failure
  }

  var all: [String: String] { lock.withLock { tokens } }

  func token(for server: String) throws -> String? {
    if let failure { throw failure }
    return lock.withLock { tokens[server] }
  }

  func save(_ token: String, for server: String) throws {
    if let failure { throw failure }
    lock.withLock { tokens[server] = token }
  }

  func remove(for server: String) throws {
    if let failure { throw failure }
    lock.withLock { tokens[server] = nil }
  }
}

/// Time for the device flow without a wait: `sleep` returns at once, records how long the
/// flow wanted to wait and moves the clock forward by that time.
final class FakeTime: @unchecked Sendable {
  private let lock = NSLock()
  private var current = Date(timeIntervalSince1970: 1_800_000_000)
  private var waits: [Duration] = []

  var slept: [Duration] { lock.withLock { waits } }

  func now() -> Date { lock.withLock { current } }

  func sleep(_ duration: Duration) {
    lock.withLock {
      waits.append(duration)
      current.addTimeInterval(TimeInterval(duration.components.seconds))
    }
  }
}

enum SignInStub {
  static let notSignedIn = StubProtocol.Response.json(#"{"error": "not signed in"}"#, status: 401)

  static func grant(interval: Int = 5, expiresIn: Int = 600) -> StubProtocol.Response {
    .json(
      """
      {"device_code": "device-1", "user_code": "BCDF-GHJK",
       "verification_uri": "http://core.test/auth/device",
       "verification_uri_complete": "http://core.test/auth/device?user_code=BCDF-GHJK",
       "expires_in": \(expiresIn), "interval": \(interval)}
      """)
  }

  static func refusal(_ name: String) -> StubProtocol.Response {
    .json(#"{"error": "\#(name)"}"#, status: 400)
  }

  static let token = StubProtocol.Response.json(
    #"{"access_token": "token-1", "token_type": "Bearer", "expires_in": 43200}"#)

  /// A device flow against a new stub host, with fake time.
  static func flow() throws -> (flow: DeviceSignIn, host: String, time: FakeTime) {
    let (host, url) = try StubProtocol.newHost()
    let time = FakeTime()
    var flow = DeviceSignIn(
      client: APIClient(baseURL: url, credential: .anonymous, session: StubProtocol.session()))
    flow.sleep = { time.sleep($0) }
    flow.now = { time.now() }
    return (flow, host, time)
  }

  /// An app whose control plane is a new stub host, with `token` kept for that host. The
  /// caller removes the defaults suite.
  @MainActor
  static func app(token: String? = nil) throws -> (
    app: AppModel, host: String, store: MemoryTokenStore, suite: String
  ) {
    let host = try StubProtocol.newHost().host
    let suite = "sign-in-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    var settings = AppSettings()
    settings.controlPlane = "http://\(host)"
    settings.save(to: defaults)
    let store = MemoryTokenStore(token.map { [key(host): $0] } ?? [:])
    let app = AppModel(defaults: defaults, session: StubProtocol.session(), tokens: store)
    return (app, host, store, suite)
  }

  /// The name of the token of a stub host in a token store.
  static func key(_ host: String) -> String { "http://\(host):80" }

  /// Makes `host` answer like a control plane with a sign-in to a caller it knows as alice.
  static func signedInAnswers(host: String) throws {
    StubProtocol.answer(host: host, path: "/api/v1/auth", .json(#"{"sign_in": true}"#))
    StubProtocol.answer(
      host: host, path: "/api/v1/me", StubProtocol.Response(body: try Fixture.data("me")))
    StubProtocol.answer(
      host: host, path: "/api/v1/users", StubProtocol.Response(body: try Fixture.data("users")))
  }
}
