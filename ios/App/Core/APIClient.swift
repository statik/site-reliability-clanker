import Foundation

/// What can go wrong when the app talks to a server. Every case has a message for the user.
enum APIError: Error, Equatable, LocalizedError {
  /// The server answered with a 4xx or 5xx status. `message` is its own text, unchanged.
  case server(status: Int, message: String)
  case decoding(path: String, detail: String)
  case transport(url: String, detail: String)
  case invalidURL(String)

  var errorDescription: String? {
    switch self {
    case .server(_, let message):
      message
    case .decoding(let path, let detail):
      "The answer of \(path) is not in the expected format: \(detail)"
    case .transport(let url, let detail):
      "Cannot reach \(url): \(detail) Check the address in Settings and that the server runs."
    case .invalidURL(let text):
      "\"\(text)\" is not a valid address. Correct it in Settings."
    }
  }

  /// A short status line next to the message, for example "HTTP 403".
  var statusLabel: String? {
    if case .server(let status, _) = self { return "HTTP \(status)" }
    return nil
  }
}

/// A small JSON-over-HTTP client. It names the demo user on every request.
struct APIClient: Sendable {
  static let userHeader = "X-Clanker-User"

  let baseURL: URL
  let user: String
  let session: URLSession

  init(baseURL: URL, user: String, session: URLSession = .shared) {
    self.baseURL = baseURL
    self.user = user
    self.session = session
  }

  func get<T: Decodable>(_ path: String, query: [URLQueryItem] = []) async throws -> T {
    try await send(try request(path, method: "GET", query: query))
  }

  func post<Body: Encodable, T: Decodable>(_ path: String, body: Body) async throws -> T {
    var request = try request(path, method: "POST", query: [])
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.httpBody = try JSONEncoder().encode(body)
    return try await send(request)
  }

  func request(_ path: String, method: String, query: [URLQueryItem]) throws -> URLRequest {
    guard
      var components = URLComponents(
        url: baseURL.appending(path: path), resolvingAgainstBaseURL: false)
    else { throw APIError.invalidURL(baseURL.absoluteString + path) }
    if !query.isEmpty { components.queryItems = query }
    guard let url = components.url else {
      throw APIError.invalidURL(baseURL.absoluteString + path)
    }
    var request = URLRequest(url: url)
    request.httpMethod = method
    request.setValue(user, forHTTPHeaderField: Self.userHeader)
    request.setValue("application/json", forHTTPHeaderField: "Accept")
    return request
  }

  private func send<T: Decodable>(_ request: URLRequest) async throws -> T {
    let path = request.url?.path ?? ""
    let data: Data
    let response: URLResponse
    do {
      (data, response) = try await session.data(for: request)
    } catch let error as URLError {
      throw APIError.transport(
        url: request.url?.absoluteString ?? path, detail: error.localizedDescription)
    }
    try Self.check(response: response, data: data)
    do {
      return try JSONDecoder().decode(T.self, from: data)
    } catch let error as DecodingError {
      throw APIError.decoding(path: path, detail: Self.describe(error))
    }
  }

  /// Turns a 4xx or 5xx answer into `APIError.server` with the server's own message.
  static func check(response: URLResponse, data: Data) throws {
    guard let http = response as? HTTPURLResponse, http.statusCode >= 400 else { return }
    struct ErrorBody: Decodable { let error: String }
    if let body = try? JSONDecoder().decode(ErrorBody.self, from: data) {
      throw APIError.server(status: http.statusCode, message: body.error)
    }
    let text = String(data: data, encoding: .utf8)?
      .trimmingCharacters(in: .whitespacesAndNewlines)
    let fallback = HTTPURLResponse.localizedString(forStatusCode: http.statusCode)
    let message = text.flatMap { $0.isEmpty ? nil : $0 } ?? fallback
    throw APIError.server(status: http.statusCode, message: message)
  }

  static func describe(_ error: DecodingError) -> String {
    switch error {
    case .keyNotFound(let key, let context):
      "the field \"\(key.stringValue)\" is missing at \(pathText(context))."
    case .typeMismatch(_, let context), .valueNotFound(_, let context):
      "the field at \(pathText(context)) has an unexpected type."
    case .dataCorrupted(let context):
      context.codingPath.isEmpty
        ? "it is not valid JSON." : "the value at \(pathText(context)) is not valid."
    @unknown default:
      error.localizedDescription
    }
  }

  private static func pathText(_ context: DecodingError.Context) -> String {
    let path = context.codingPath.map(\.stringValue).joined(separator: ".")
    return path.isEmpty ? "the top level" : path
  }
}

/// Turns any error into text for the user. Server messages are passed through unchanged.
func userMessage(_ error: Error) -> String {
  (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
}
