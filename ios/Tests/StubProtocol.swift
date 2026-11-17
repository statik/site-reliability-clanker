import Foundation

@testable import PaperboatManager

/// Answers requests of a URLSession from memory. Each test uses its own host name, so tests
/// can run in parallel.
final class StubProtocol: URLProtocol, @unchecked Sendable {
  struct Response: Sendable {
    var status = 200
    var body = Data()
  }

  struct Recorded: Sendable {
    let method: String
    let path: String
    let query: String
    let headers: [String: String]
    let body: Data
  }

  private static let lock = NSLock()
  nonisolated(unsafe) private static var responses: [String: Response] = [:]
  nonisolated(unsafe) private static var recorded: [String: [Recorded]] = [:]

  /// Returns a client whose requests are answered with `response`, and the host to query.
  static func client(
    user: String = "alice", status: Int = 200, body: String
  ) throws -> (client: APIClient, host: String) {
    let host = "stub-\(UUID().uuidString.lowercased()).test"
    lock.withLock {
      responses[host] = Response(status: status, body: Data(body.utf8))
    }
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [StubProtocol.self]
    guard let url = URL(string: "http://\(host)") else {
      throw APIError.invalidURL(host)
    }
    let client = APIClient(
      baseURL: url, user: user, session: URLSession(configuration: configuration))
    return (client, host)
  }

  static func requests(to host: String) -> [Recorded] {
    lock.withLock { recorded[host] ?? [] }
  }

  override static func canInit(with request: URLRequest) -> Bool {
    request.url?.host()?.hasPrefix("stub-") ?? false
  }

  override static func canonicalRequest(for request: URLRequest) -> URLRequest { request }

  override func startLoading() {
    guard let url = request.url, let host = url.host() else {
      client?.urlProtocol(self, didFailWithError: URLError(.badURL))
      return
    }
    let entry = Recorded(
      method: request.httpMethod ?? "", path: url.path(), query: url.query() ?? "",
      headers: request.allHTTPHeaderFields ?? [:], body: Self.readBody(of: request))
    let response = Self.lock.withLock {
      Self.recorded[host, default: []].append(entry)
      return Self.responses[host] ?? Response(status: 500)
    }
    guard
      let http = HTTPURLResponse(
        url: url, statusCode: response.status, httpVersion: "HTTP/1.1",
        headerFields: ["Content-Type": "application/json"])
    else {
      client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
      return
    }
    client?.urlProtocol(self, didReceive: http, cacheStoragePolicy: .notAllowed)
    client?.urlProtocol(self, didLoad: response.body)
    client?.urlProtocolDidFinishLoading(self)
  }

  override func stopLoading() {}

  /// URLSession hands the body of a request to a protocol as a stream.
  private static func readBody(of request: URLRequest) -> Data {
    if let body = request.httpBody { return body }
    guard let stream = request.httpBodyStream else { return Data() }
    stream.open()
    defer { stream.close() }
    var data = Data()
    var buffer = [UInt8](repeating: 0, count: 4096)
    while stream.hasBytesAvailable {
      let count = stream.read(&buffer, maxLength: buffer.count)
      if count <= 0 { break }
      data.append(buffer, count: count)
    }
    return data
  }
}
