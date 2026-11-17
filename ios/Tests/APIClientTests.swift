import Foundation
import Testing

@testable import PaperboatManager

@Suite("API client")
struct APIClientTests {
  @Test func everyGetNamesTheUserInTheHeader() async throws {
    let (client, host) = try StubProtocol.client(credential: .demoUser("bob"), body: "[]")
    let api = ControlPlaneAPI(client: client)
    _ = try await api.workItems()
    _ = try await api.chat(workItem: "WI-0020")
    let requests = StubProtocol.requests(to: host)
    #expect(requests.count == 2)
    #expect(requests.allSatisfy { $0.headers[APIClient.userHeader] == "bob" })
    #expect(requests.map(\.path) == ["/api/v1/work-items", "/api/v1/chat"])
    #expect(requests.last?.query == "work_item=WI-0020")
  }

  @Test func decisionIsPostedWithUserHeaderPathAndBody() async throws {
    let item = #"{"id": "WI-0020", "state": "Approved"}"#
    let (client, host) = try StubProtocol.client(credential: .demoUser("alice"), body: item)
    let request = DecisionRequest(decision: .approve, comment: "  looks right \n")
    let answer = try await ControlPlaneAPI(client: client).decide("WI-0020", request)
    #expect(answer.state == .approved)
    let sent = try #require(StubProtocol.requests(to: host).first)
    #expect(sent.method == "POST")
    #expect(sent.path == "/api/v1/work-items/WI-0020/decision")
    #expect(sent.headers[APIClient.userHeader] == "alice")
    #expect(sent.headers["Content-Type"] == "application/json")
    let body = try JSONDecoder().decode([String: String].self, from: sent.body)
    #expect(body == ["decision": "approve", "comment": "looks right", "scheduled_for": ""])
  }

  @Test func chatQuestionIsPostedWithItsScope() async throws {
    let entry = #"{"id": 4, "role": "assistant", "body": "Two replicas.", "citations": null}"#
    let (client, host) = try StubProtocol.client(body: entry)
    let answer = try await ControlPlaneAPI(client: client).ask(workItem: "WI-0020", question: "?")
    #expect(answer.isAssistant)
    #expect(answer.citations.isEmpty)
    let sent = try #require(StubProtocol.requests(to: host).first)
    let body = try JSONDecoder().decode([String: String].self, from: sent.body)
    #expect(body == ["work_item": "WI-0020", "question": "?"])
  }

  @Test func forbiddenAnswerGivesTheServerMessageVerbatim() async throws {
    let message =
      "dave has role viewer and cannot decide on WI-0020: an approver or admin must decide"
    let (client, _) = try StubProtocol.client(
      credential: .demoUser("dave"), status: 403, body: #"{"error": "\#(message)"}"#)
    let request = DecisionRequest(decision: .requestChanges, comment: "")
    do {
      _ = try await ControlPlaneAPI(client: client).decide("WI-0020", request)
      Issue.record("The call must throw")
    } catch let error as APIError {
      #expect(error == .server(status: 403, message: message))
      #expect(userMessage(error) == message)
      #expect(error.statusLabel == "HTTP 403")
    }
  }

  @Test func errorAnswerWithoutJSONBodyStillGivesAMessage() async throws {
    let (client, _) = try StubProtocol.client(status: 502, body: "upstream is down\n")
    await #expect(throws: APIError.server(status: 502, message: "upstream is down")) {
      _ = try await ControlPlaneAPI(client: client).overview()
    }
    let (empty, _) = try StubProtocol.client(status: 500, body: "")
    do {
      _ = try await ControlPlaneAPI(client: empty).overview()
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .server(let status, let message) = error else {
        Issue.record("Expected a server error, got \(error)")
        return
      }
      #expect(status == 500)
      #expect(!message.isEmpty)
    }
  }

  @Test func malformedJSONIsADecodingErrorThatNamesThePath() async throws {
    let (client, _) = try StubProtocol.client(body: "{not json")
    do {
      _ = try await ControlPlaneAPI(client: client).overview()
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .decoding(let path, let detail) = error else {
        Issue.record("Expected a decoding error, got \(error)")
        return
      }
      #expect(path == "/api/v1/overview")
      #expect(detail == "it is not valid JSON.")
      #expect(userMessage(error).contains("/api/v1/overview"))
    }
  }

  @Test func wrongShapeIsADecodingErrorThatNamesTheField() async throws {
    let (client, _) = try StubProtocol.client(body: #"[{"title": "no id"}]"#)
    do {
      _ = try await ControlPlaneAPI(client: client).workItems()
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .decoding(_, let detail) = error else {
        Issue.record("Expected a decoding error, got \(error)")
        return
      }
      #expect(detail.contains("\"id\" is missing"))
    }
  }

  @Test func connectionRefusedIsATransportErrorWithAdvice() async throws {
    // Port 1 on the loopback interface has no listener.
    let url = try AppSettings.baseURL("http://127.0.0.1:1")
    let client = APIClient(
      baseURL: url, credential: .demoUser("alice"),
      session: URLSession(configuration: .ephemeral))
    do {
      _ = try await ControlPlaneAPI(client: client).overview()
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .transport(let failedURL, _) = error else {
        Issue.record("Expected a transport error, got \(error)")
        return
      }
      #expect(failedURL == "http://127.0.0.1:1/api/v1/overview")
      #expect(userMessage(error).contains("Check the address in Settings"))
    }
  }

  @Test func baseAddressesAreValidated() throws {
    #expect(try AppSettings.baseURL(" http://127.0.0.1:7070 ").absoluteString
      == "http://127.0.0.1:7070")
    #expect(throws: APIError.invalidURL("ftp://example.test")) {
      try AppSettings.baseURL("ftp://example.test")
    }
    #expect(throws: APIError.invalidURL("localhost")) {
      try AppSettings.baseURL("localhost")
    }
    #expect(throws: APIError.invalidURL("")) { try AppSettings.baseURL("") }
  }

  @Test func settingsRoundTripThroughUserDefaults() throws {
    let name = "settings-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: name))
    defer { defaults.removePersistentDomain(forName: name) }
    #expect(AppSettings.load(from: defaults) == AppSettings())
    #expect(AppSettings().user == "dave")
    var settings = AppSettings()
    settings.controlPlane = "http://10.0.0.5:7070"
    settings.user = "carol"
    settings.save(to: defaults)
    #expect(AppSettings.load(from: defaults) == settings)
  }
}

@Suite("Decision request encoding")
struct DecisionRequestTests {
  private func json(_ request: DecisionRequest) throws -> [String: String] {
    try JSONDecoder().decode([String: String].self, from: JSONEncoder().encode(request))
  }

  @Test func approveNowSendsAnEmptySchedule() throws {
    let body = try json(DecisionRequest(decision: .approve, comment: ""))
    #expect(body == ["decision": "approve", "comment": "", "scheduled_for": ""])
  }

  @Test func scheduledApprovalSendsUTCWithMilliseconds() throws {
    let date = try #require(Timestamps.parse("2026-12-16T03:30:00.000Z"))
    let body = try json(
      DecisionRequest(decision: .approve, comment: "window", scheduledFor: date))
    #expect(body["scheduled_for"] == "2026-12-16T03:30:00.000Z")
    #expect(body["decision"] == "approve")
  }

  @Test func rejectAndRequestChangesUseTheServerNames() throws {
    #expect(try json(DecisionRequest(decision: .reject, comment: "no"))["decision"] == "reject")
    #expect(
      try json(DecisionRequest(decision: .requestChanges, comment: ""))["decision"]
        == "request_changes")
  }

  @Test func choicesMapToServerDecisions() {
    #expect(DecisionChoice.approve.kind == .approve)
    #expect(DecisionChoice.schedule.kind == .approve)
    #expect(DecisionChoice.requestChanges.kind == .requestChanges)
    #expect(DecisionChoice.reject.kind == .reject)
  }

  @Test func timestampsParseBothServerFormats() throws {
    let withFraction = try #require(Timestamps.parse("2026-12-16T02:27:31.613Z"))
    let without = try #require(Timestamps.parse("2026-12-16T02:27:31Z"))
    #expect(abs(withFraction.timeIntervalSince(without) - 0.613) < 0.001)
    #expect(Timestamps.parse("") == nil)
    #expect(Timestamps.parse("yesterday") == nil)
    #expect(Timestamps.display("2026-12-16T02:07:20.303Z") == "16 Dec 2026, 02:07 UTC")
    #expect(Timestamps.display("not a time") == "not a time")
    #expect(
      Timestamps.span(from: "2026-12-16T02:07:20.303Z", to: "2026-12-16T02:37:20.303Z")
        == "30 minutes")
  }
}
