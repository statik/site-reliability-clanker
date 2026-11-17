import Foundation

/// One message of a server-sent event stream.
struct SSEMessage: Equatable, Sendable {
  var id = ""
  var event = "message"
  var data = ""
}

/// Parses the text/event-stream format, line by line. A blank line ends a message.
struct SSEParser: Sendable {
  private var pending = SSEMessage()
  private var dataLines: [String] = []
  private var hasFields = false

  /// Feeds one line without its line ending. Returns a message when the line completes one.
  mutating func feed(line: String) -> SSEMessage? {
    if line.isEmpty { return flush() }
    if line.hasPrefix(":") { return nil }
    let field: Substring
    var value: Substring
    if let colon = line.firstIndex(of: ":") {
      field = line[..<colon]
      value = line[line.index(after: colon)...]
      if value.hasPrefix(" ") { value = value.dropFirst() }
    } else {
      field = Substring(line)
      value = ""
    }
    switch field {
    case "id": pending.id = String(value)
    case "event": pending.event = String(value)
    case "data": dataLines.append(String(value))
    default: return nil
    }
    hasFields = true
    return nil
  }

  private mutating func flush() -> SSEMessage? {
    defer {
      pending = SSEMessage()
      dataLines = []
      hasFields = false
    }
    guard hasFields, !dataLines.isEmpty else { return nil }
    pending.data = dataLines.joined(separator: "\n")
    return pending
  }
}

/// Splits a byte stream into lines and keeps the empty ones. `URLSession.AsyncBytes.lines`
/// drops empty lines, and the stream format needs them to end a message.
struct LineSplitter: Sendable {
  private var buffer: [UInt8] = []

  /// Feeds one byte. Returns a line when the byte ends one.
  mutating func feed(_ byte: UInt8) -> String? {
    if byte == UInt8(ascii: "\n") {
      if buffer.last == UInt8(ascii: "\r") { buffer.removeLast() }
      let line = String(decoding: buffer, as: UTF8.self)
      buffer.removeAll(keepingCapacity: true)
      return line
    }
    buffer.append(byte)
    return nil
  }
}

/// Remembers how far a screen has read the buffer of stream events. Several events can
/// arrive between two updates of a screen; the cursor returns all of them, in order.
struct EventCursor: Sendable, Equatable {
  private(set) var lastSeq = 0

  mutating func take(from buffer: [AuditEvent]) -> [AuditEvent] {
    let fresh = buffer.filter { $0.seq > lastSeq }.sorted { $0.seq < $1.seq }
    lastSeq = max(lastSeq, fresh.last?.seq ?? 0)
    return fresh
  }
}

/// What the event stream reports to the app.
enum StreamUpdate: Sendable {
  case connected
  case event(AuditEvent)
  /// The connection ended or failed. The reader waits and connects again.
  case disconnected(reason: String)
  /// The stream sent a message that is not an audit event in the documented format.
  case unreadable(detail: String)
}

/// Reads `GET /api/v1/events/stream` and reconnects when the connection ends. The server
/// replays nothing, so after a reconnect the reader fetches the events it missed.
struct EventStreamReader: Sendable {
  let api: ControlPlaneAPI
  var retryDelay: Duration = .seconds(3)

  static func streamSession() -> URLSession {
    let configuration = URLSessionConfiguration.default
    // The server sends nothing while no event happens. Do not time the request out.
    configuration.timeoutIntervalForRequest = 60 * 60 * 24
    configuration.timeoutIntervalForResource = 60 * 60 * 24 * 7
    return URLSession(configuration: configuration)
  }

  func updates() -> AsyncStream<StreamUpdate> {
    AsyncStream { continuation in
      let task = Task {
        let session = Self.streamSession()
        var lastSeq = 0
        while !Task.isCancelled {
          do {
            try await readOnce(session: session, lastSeq: &lastSeq, into: continuation)
            continuation.yield(.disconnected(reason: "The server closed the event stream."))
          } catch is CancellationError {
            break
          } catch {
            continuation.yield(.disconnected(reason: userMessage(error)))
          }
          try? await Task.sleep(for: retryDelay)
        }
        session.invalidateAndCancel()
        continuation.finish()
      }
      continuation.onTermination = { _ in task.cancel() }
    }
  }

  private func readOnce(
    session: URLSession, lastSeq: inout Int,
    into continuation: AsyncStream<StreamUpdate>.Continuation
  ) async throws {
    var request = try api.client.request("/api/v1/events/stream", method: "GET", query: [])
    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
    let bytes: URLSession.AsyncBytes
    let response: URLResponse
    do {
      (bytes, response) = try await session.bytes(for: request)
    } catch let error as URLError {
      throw APIError.transport(
        url: request.url?.absoluteString ?? "", detail: error.localizedDescription)
    }
    try APIClient.check(response: response, data: Data())
    continuation.yield(.connected)
    if lastSeq > 0 {
      for event in try await api.events(after: lastSeq) {
        lastSeq = max(lastSeq, event.seq)
        continuation.yield(.event(event))
      }
    }
    var splitter = LineSplitter()
    var parser = SSEParser()
    for try await byte in bytes {
      guard let line = splitter.feed(byte), let message = parser.feed(line: line) else {
        continue
      }
      guard message.event == "audit" else { continue }
      do {
        let event = try Self.decode(message)
        if event.seq <= lastSeq { continue }
        lastSeq = event.seq
        continuation.yield(.event(event))
      } catch let error as DecodingError {
        continuation.yield(.unreadable(detail: APIClient.describe(error)))
      }
    }
  }

  /// Decodes the audit event that one stream message carries.
  static func decode(_ message: SSEMessage) throws -> AuditEvent {
    try JSONDecoder().decode(AuditEvent.self, from: Data(message.data.utf8))
  }
}
