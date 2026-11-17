import Foundation
import Observation

/// The sign-in screen: one run of the device flow.
@MainActor
@Observable
final class SignInModel {
  enum Step: Equatable, Sendable {
    case idle
    case requestingCode
    /// The person has the code and the app asks for its token.
    case waiting(DeviceGrant)
    case failed(message: String)
  }

  private(set) var step = Step.idle
  private var task: Task<Void, Never>?
  /// Replaces the wait between two questions for the token. Tests set it.
  private let sleep: (@Sendable (Duration) async throws -> Void)?

  init(sleep: (@Sendable (Duration) async throws -> Void)? = nil) {
    self.sleep = sleep
  }

  func begin(app: AppModel) {
    task?.cancel()
    task = Task { await run(app: app) }
  }

  func cancel() {
    task?.cancel()
    step = .idle
  }

  /// Asks for a code, waits for the decision of the person and hands the token to the app.
  func run(app: AppModel) async {
    step = .requestingCode
    let server = app.settings.controlPlane
    do {
      var flow = try app.deviceSignIn()
      if let sleep { flow.sleep = sleep }
      let grant = try await flow.requestCode()
      step = .waiting(grant)
      let token = try await flow.waitForToken(grant)
      step = .idle
      // A token is for the server that gave it. Drop it if the address changed meanwhile.
      guard server == app.settings.controlPlane else { return }
      await app.completeSignIn(token: token)
    } catch is CancellationError {
      step = .idle
    } catch {
      step = .failed(message: userMessage(error))
    }
  }
}
