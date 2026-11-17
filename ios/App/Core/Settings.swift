import Foundation

/// The addresses of the two servers and the chosen demo user, kept in UserDefaults.
struct AppSettings: Equatable, Sendable {
  static let defaultControlPlane = "http://127.0.0.1:7070"
  static let defaultPaperboat = "http://localhost:8088"
  static let defaultUser = "dave"

  var controlPlane = AppSettings.defaultControlPlane
  var paperboat = AppSettings.defaultPaperboat
  /// The name sent in `X-Clanker-User`. The default is the viewer, who cannot decide.
  var user = AppSettings.defaultUser

  private enum Key {
    static let controlPlane = "controlPlaneURL"
    static let paperboat = "paperboatURL"
    static let user = "signedInUser"
  }

  static func load(from defaults: UserDefaults) -> AppSettings {
    var settings = AppSettings()
    if let value = defaults.string(forKey: Key.controlPlane), !value.isEmpty {
      settings.controlPlane = value
    }
    if let value = defaults.string(forKey: Key.paperboat), !value.isEmpty {
      settings.paperboat = value
    }
    if let value = defaults.string(forKey: Key.user), !value.isEmpty {
      settings.user = value
    }
    return settings
  }

  func save(to defaults: UserDefaults) {
    defaults.set(controlPlane, forKey: Key.controlPlane)
    defaults.set(paperboat, forKey: Key.paperboat)
    defaults.set(user, forKey: Key.user)
  }

  /// Reads a base address typed by the user. Only http and https with a host are valid.
  static func baseURL(_ text: String) throws -> URL {
    let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
    guard let url = URL(string: trimmed), let scheme = url.scheme, url.host() != nil,
      scheme == "http" || scheme == "https"
    else { throw APIError.invalidURL(trimmed) }
    return url
  }
}

/// Launch arguments for development: they open a screen without taps, for screenshots.
/// They work only in DEBUG builds. Example: `-initialScreen approvals -workItem WI-0020`.
struct LaunchOptions: Equatable, Sendable {
  var initialScreen = ""
  var workItem = ""
  var user = ""
  var scrollTo = ""
  /// "approve", "schedule", "request_changes" or "reject": opens that decision form.
  var decision = ""
  /// Submits the opened decision. Refused unless the chosen user is a viewer.
  var submitDecision = false
  var job = 0
  var isUnderTest = false

  static func read(_ defaults: UserDefaults = .standard) -> LaunchOptions {
    var options = LaunchOptions()
    options.isUnderTest = NSClassFromString("XCTestCase") != nil
    #if DEBUG
      options.initialScreen = defaults.string(forKey: "initialScreen") ?? ""
      options.workItem = defaults.string(forKey: "workItem") ?? ""
      options.user = defaults.string(forKey: "debugUser") ?? ""
      options.scrollTo = defaults.string(forKey: "debugScroll") ?? ""
      options.decision = defaults.string(forKey: "debugDecision") ?? ""
      options.submitDecision = defaults.bool(forKey: "debugSubmit")
      options.job = defaults.integer(forKey: "debugJob")
    #endif
    return options
  }
}
