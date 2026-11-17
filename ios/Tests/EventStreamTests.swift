import Foundation
import Testing

@testable import PaperboatManager

@Suite("Server-sent events")
struct EventStreamTests {
  private func messages(from text: String) -> [SSEMessage] {
    var splitter = LineSplitter()
    var parser = SSEParser()
    var out: [SSEMessage] = []
    for byte in Array(text.utf8) {
      if let line = splitter.feed(byte), let message = parser.feed(line: line) {
        out.append(message)
      }
    }
    return out
  }

  @Test func parsesTheFormatTheControlPlaneSends() throws {
    let stream = """
      : connected

      id: 455
      event: audit
      data: {"seq":455,"ts":"2026-12-16T02:40:00.000Z","type":"apply.started",\
      "actor":"runner","layer":"50-app","work_item":"WI-0020","summary":"Apply of 50-app started",\
      "data":{"grant":"G-1"}}


      """
    let parsed = messages(from: stream)
    #expect(parsed.count == 1)
    let message = try #require(parsed.first)
    #expect(message.id == "455")
    #expect(message.event == "audit")
    let event = try EventStreamReader.decode(message)
    #expect(event.seq == 455)
    #expect(event.type == "apply.started")
    #expect(event.workItem == "WI-0020")
    #expect(event.string("grant") == "G-1")
  }

  @Test func aMessageIsDispatchedOnlyByTheBlankLine() {
    var parser = SSEParser()
    #expect(parser.feed(line: "event: audit") == nil)
    #expect(parser.feed(line: "data: {}") == nil)
    #expect(parser.feed(line: "") == SSEMessage(id: "", event: "audit", data: "{}"))
    // A second blank line has nothing to dispatch.
    #expect(parser.feed(line: "") == nil)
  }

  @Test func commentsAndUnknownFieldsAreIgnored() {
    var parser = SSEParser()
    #expect(parser.feed(line: ": keep-alive") == nil)
    #expect(parser.feed(line: "retry: 3000") == nil)
    #expect(parser.feed(line: "") == nil)
  }

  @Test func severalDataLinesAreJoinedAndTheDefaultEventIsMessage() {
    var parser = SSEParser()
    _ = parser.feed(line: "data: first")
    _ = parser.feed(line: "data:second")
    #expect(parser.feed(line: "") == SSEMessage(id: "", event: "message", data: "first\nsecond"))
  }

  @Test func stateDoesNotLeakIntoTheNextMessage() {
    let parsed = messages(from: "id: 1\nevent: audit\ndata: a\n\ndata: b\n\n")
    #expect(parsed == [
      SSEMessage(id: "1", event: "audit", data: "a"),
      SSEMessage(id: "", event: "message", data: "b"),
    ])
  }

  @Test func lineSplitterKeepsEmptyLinesAndAcceptsCarriageReturns() {
    var splitter = LineSplitter()
    var lines: [String] = []
    for byte in Array("a\r\n\r\nb\n\nlast".utf8) {
      if let line = splitter.feed(byte) { lines.append(line) }
    }
    // "last" has no line ending yet, so it is not a line.
    #expect(lines == ["a", "", "b", ""])
  }

  @Test func lineSplitterHandlesMultiByteCharactersAcrossFeeds() {
    let parsed = messages(from: "data: Detected → Drafting\n\n")
    #expect(parsed.first?.data == "Detected → Drafting")
  }

  @Test func aMessageThatIsNotAnAuditEventFailsToDecode() {
    #expect(throws: DecodingError.self) {
      try EventStreamReader.decode(SSEMessage(id: "1", event: "audit", data: "{\"seq\": \"x\"}"))
    }
    #expect(throws: DecodingError.self) {
      try EventStreamReader.decode(SSEMessage(id: "1", event: "audit", data: "not json"))
    }
  }
}
