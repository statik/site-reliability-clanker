import Foundation
import Testing

@testable import SRC

@Suite("Device flow")
struct DeviceSignInTests {
  private func grant(interval: Int = 5, expiresIn: Int = 600) throws -> DeviceGrant {
    try JSONDecoder().decode(
      DeviceGrant.self, from: SignInStub.grant(interval: interval, expiresIn: expiresIn).body)
  }

  @Test func aCodeRequestIsAnOpenFormPostAndReadsEveryField() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/code", SignInStub.grant())
    let grant = try await flow.requestCode()
    #expect(grant.deviceCode == "device-1")
    #expect(grant.userCode == "BCDF-GHJK")
    #expect(grant.page.absoluteString == "http://core.test/auth/device")
    #expect(
      grant.pageWithCode.absoluteString == "http://core.test/auth/device?user_code=BCDF-GHJK")
    #expect(grant.expiresIn == 600)
    #expect(grant.interval == 5)
    let sent = try #require(StubProtocol.requests(to: host).first)
    #expect(sent.method == "POST")
    #expect(sent.path == "/auth/device/code")
    #expect(sent.headers["Authorization"] == nil)
    #expect(sent.headers[APIClient.userHeader] == nil)
  }

  @Test func aPendingCodeIsAskedAgainAtTheIntervalUntilItIsApproved() async throws {
    let (flow, host, time) = try SignInStub.flow()
    StubProtocol.answer(
      host: host, path: "/auth/device/token", SignInStub.refusal("authorization_pending"),
      SignInStub.refusal("authorization_pending"), SignInStub.token)
    let token = try await flow.waitForToken(try grant(interval: 7))
    #expect(token == "token-1")
    #expect(time.slept == [.seconds(7), .seconds(7), .seconds(7)])
    let sent = StubProtocol.requests(to: host)
    #expect(sent.count == 3)
    #expect(sent.allSatisfy { $0.method == "POST" && $0.path == "/auth/device/token" })
    #expect(sent.allSatisfy { String(decoding: $0.body, as: UTF8.self) == "device_code=device-1" })
    #expect(
      sent.allSatisfy { $0.headers["Content-Type"] == "application/x-www-form-urlencoded" })
  }

  @Test func theAppNeverAsksFasterThanOnceASecond() async throws {
    let (flow, host, time) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/token", SignInStub.token)
    _ = try await flow.waitForToken(try grant(interval: 0))
    #expect(time.slept == [.seconds(1)])
  }

  @Test func aRefusedCodeEndsTheFlow() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(
      host: host, path: "/auth/device/token", SignInStub.refusal("authorization_pending"),
      SignInStub.refusal("access_denied"))
    await #expect(throws: SignInError.denied) { _ = try await flow.waitForToken(try grant()) }
    #expect(StubProtocol.requests(to: host).count == 2)
    #expect(userMessage(SignInError.denied).contains("refused in the dashboard"))
  }

  @Test func aCodeThatTheServerEndedEndsTheFlow() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(
      host: host, path: "/auth/device/token", SignInStub.refusal("expired_token"))
    await #expect(throws: SignInError.expired) { _ = try await flow.waitForToken(try grant()) }
    #expect(userMessage(SignInError.expired).contains("Sign in again"))
  }

  @Test func anErrorNameThatTheAppDoesNotKnowEndsTheFlowAndIsNamed() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/token", SignInStub.refusal("slow_down"))
    await #expect(throws: SignInError.refused(name: "slow_down")) {
      _ = try await flow.waitForToken(try grant())
    }
    #expect(StubProtocol.requests(to: host).count == 1)
    #expect(userMessage(SignInError.refused(name: "slow_down")).contains("(slow_down)"))
  }

  @Test func aRequestThatDoesNotReachTheServerIsTriedAgain() async throws {
    let (flow, host, time) = try SignInStub.flow()
    StubProtocol.answer(
      host: host, path: "/auth/device/token", .failure(.networkConnectionLost),
      .failure(.notConnectedToInternet), SignInStub.token)
    #expect(try await flow.waitForToken(try grant()) == "token-1")
    #expect(time.slept.count == 3)
  }

  @Test func theAppStopsWhenTheLifeOfTheCodeIsOverByItsOwnClock() async throws {
    let (flow, host, time) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/token", .failure(.timedOut))
    await #expect(throws: SignInError.expired) {
      _ = try await flow.waitForToken(try grant(interval: 5, expiresIn: 12))
    }
    #expect(time.slept == [.seconds(5), .seconds(5), .seconds(5)])
    #expect(StubProtocol.requests(to: host).count == 2)
  }

  @Test func aServerFailureDuringTheWaitKeepsTheMessageOfTheServer() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(
      host: host, path: "/auth/device/token",
      StubProtocol.Response(status: 500, body: Data("read the code: no database\n".utf8)))
    await #expect(throws: APIError.server(status: 500, message: "read the code: no database")) {
      _ = try await flow.waitForToken(try grant())
    }
  }

  @Test func anAnswerWithoutATokenIsABadResponse() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/token", .json(#"{"token_type": "x"}"#))
    do {
      _ = try await flow.waitForToken(try grant())
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .decoding(let path, let detail) = error else {
        Issue.record("Expected a decoding error, got \(error)")
        return
      }
      #expect(path == "/auth/device/token")
      #expect(detail.contains("access_token"))
    }
    StubProtocol.answer(host: host, path: "/auth/device/token", .json(#"{"access_token": ""}"#))
    await #expect(
      throws: APIError.decoding(path: "/auth/device/token", detail: "the token is empty.")
    ) { _ = try await flow.waitForToken(try grant()) }
  }

  @Test(arguments: [
    StubProtocol.Response(status: 200, body: Data("<!doctype html><html></html>".utf8)),
    StubProtocol.Response(status: 404, body: Data("404 page not found\n".utf8)),
    StubProtocol.Response.json(
      #"{"device_code": "", "user_code": "", "expires_in": 600, "interval": 5, "#
        + #""verification_uri": "http://a.test", "verification_uri_complete": "http://a.test"}"#
    ),
  ])
  func aServerThatGivesNoCodeIsNamedWithAdvice(answer: StubProtocol.Response) async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/code", answer)
    do {
      _ = try await flow.requestCode()
      Issue.record("The call must throw")
    } catch let error as SignInError {
      guard case .noSignIn(let server, _) = error else {
        Issue.record("Expected noSignIn, got \(error)")
        return
      }
      #expect(server == "http://\(host)")
      #expect(userMessage(error).contains("Check in Settings"))
    }
  }

  @Test func aCodeRequestThatDoesNotReachTheServerIsATransportError() async throws {
    let (flow, host, _) = try SignInStub.flow()
    StubProtocol.answer(host: host, path: "/auth/device/code", .failure(.cannotConnectToHost))
    do {
      _ = try await flow.requestCode()
      Issue.record("The call must throw")
    } catch let error as APIError {
      guard case .transport = error else {
        Issue.record("Expected a transport error, got \(error)")
        return
      }
    }
  }

  @Test func formFieldsAreEscaped() {
    #expect(APIClient.formEncoded([:]) == "")
    #expect(
      APIClient.formEncoded(["device_code": "a+b/c=d&e f", "b": "_-x"])
        == "b=_-x&device_code=a%2Bb%2Fc%3Dd%26e%20f")
  }
}

@Suite("Token store")
struct TokenStoreTests {
  @Test func theServerKeyIsTheSameForEverySpellingOfOneAddress() throws {
    #expect(AppSettings.serverKey(try AppSettings.baseURL("http://Core.Example.test/"))
      == "http://core.example.test:80")
    #expect(AppSettings.serverKey(try AppSettings.baseURL("http://core.example.test:80"))
      == "http://core.example.test:80")
    #expect(AppSettings.serverKey(try AppSettings.baseURL("https://core.example.test"))
      == "https://core.example.test:443")
    #expect(AppSettings.serverKey(try AppSettings.baseURL("http://127.0.0.1:7270"))
      == "http://127.0.0.1:7270")
    #expect(AppSettings.serverKey(try AppSettings.baseURL("http://127.0.0.1:7070"))
      != AppSettings.serverKey(try AppSettings.baseURL("http://127.0.0.1:7270")))
  }
}
