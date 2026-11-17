import Foundation

/// The answer of `POST /auth/device/code`: a code for the person and a code for the app.
struct DeviceGrant: Decodable, Sendable, Equatable {
  /// The secret of the app. With it the app asks for its token.
  let deviceCode: String
  /// The short code that the person compares with the page in the browser.
  let userCode: String
  /// The page where a signed-in person approves a code.
  let page: URL
  /// The same page with the code filled in.
  let pageWithCode: URL
  /// How many seconds the person has to approve the code.
  let expiresIn: Int
  /// How many seconds the app must wait between two questions for its token.
  let interval: Int

  enum CodingKeys: String, CodingKey {
    case deviceCode = "device_code"
    case userCode = "user_code"
    case page = "verification_uri"
    case pageWithCode = "verification_uri_complete"
    case expiresIn = "expires_in"
    case interval
  }
}

/// Why a sign-in with the device flow gave no token. Every case has a message for the user.
enum SignInError: Error, Equatable, LocalizedError {
  case denied
  case expired
  /// The server ended the sign-in with an error name that this app does not know.
  case refused(name: String)
  /// The address does not answer like a control plane with a sign-in.
  case noSignIn(server: String, detail: String)

  var errorDescription: String? {
    switch self {
    case .denied:
      "The code was refused in the dashboard. Sign in again if that was a mistake."
    case .expired:
      "The code ended before a person approved it. Sign in again to get a new code."
    case .refused(let name):
      "The control plane ended the sign-in (\(name)). Sign in again to get a new code."
    case .noSignIn(let server, let detail):
      "\(server) gave no sign-in code: \(detail) Check in Settings that the address is "
        + "that of a control plane with a sign-in."
    }
  }
}

/// The client side of the OAuth 2.0 device authorization grant, as the control plane
/// implements it in `internal/signin/device.go`.
struct DeviceSignIn: Sendable {
  /// A client without a credential: the two endpoints of the flow are open to everybody.
  let client: APIClient
  var sleep: @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
  var now: @Sendable () -> Date = { Date() }

  private struct TokenAnswer: Decodable {
    let accessToken: String

    enum CodingKeys: String, CodingKey { case accessToken = "access_token" }
  }

  func requestCode() async throws -> DeviceGrant {
    let grant: DeviceGrant
    do {
      grant = try await client.postForm("/auth/device/code", fields: [:])
    } catch let error as APIError {
      switch error {
      case .server, .decoding:
        throw SignInError.noSignIn(
          server: client.baseURL.absoluteString, detail: userMessage(error))
      case .transport, .invalidURL:
        throw error
      }
    }
    guard !grant.deviceCode.isEmpty, !grant.userCode.isEmpty else {
      throw SignInError.noSignIn(
        server: client.baseURL.absoluteString, detail: "The answer has an empty code.")
    }
    return grant
  }

  /// Asks for the token at the interval of the grant until a person approved or refused
  /// the code, or the code ended. A request that does not reach the server is tried again:
  /// the phone can lose the connection while the browser is in front.
  func waitForToken(_ grant: DeviceGrant) async throws -> String {
    let deadline = now().addingTimeInterval(TimeInterval(grant.expiresIn))
    let interval = Duration.seconds(max(grant.interval, 1))
    while true {
      try await sleep(interval)
      guard now() < deadline else { throw SignInError.expired }
      do {
        let answer: TokenAnswer = try await client.postForm(
          "/auth/device/token", fields: ["device_code": grant.deviceCode])
        guard !answer.accessToken.isEmpty else {
          throw APIError.decoding(path: "/auth/device/token", detail: "the token is empty.")
        }
        return answer.accessToken
      } catch APIError.transport {
        continue
      } catch APIError.server(status: 400, message: let name) {
        switch name {
        case "authorization_pending": continue
        case "access_denied": throw SignInError.denied
        case "expired_token": throw SignInError.expired
        default: throw SignInError.refused(name: name)
        }
      }
    }
  }
}
