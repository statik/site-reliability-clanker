import Foundation
import Testing

@testable import SRC

@Suite("Decoding of the fixtures captured from the live servers")
struct DecodingTests {
  @Test func overviewHasEnvironmentWaitingItemsAndChain() throws {
    let overview = try Fixture.decode(Overview.self, "overview")
    let environment = try #require(overview.environments.first)
    #expect(environment.name == "local")
    #expect(environment.deployed)
    #expect(environment.versions["paperboat"] == "1.1.1")
    #expect(environment.health?.total == 8)
    #expect(overview.waiting.map(\.id) == ["WI-0020"])
    #expect(overview.chain.ok)
    #expect(overview.chain.count > 400)
    #expect(overview.catchUp.narrative.hasPrefix("Since 2026-12-02"))
    #expect(Timestamps.parse(overview.now) != nil)
  }

  @Test func workItemListDecodesEveryItemIncludingIncidentsWithoutTier() throws {
    let items = try Fixture.decode([WorkItem].self, "work_items")
    #expect(items.count == 21)
    let incident = try #require(items.first { $0.id == "WI-0018" })
    #expect(incident.riskTier == .none)
    #expect(incident.state == .failed)
    #expect(incident.layers.isEmpty)
    // Go sends `"pre_checks": null` when the explanation was never written.
    #expect(incident.explanation.preChecks.isEmpty)
    #expect(incident.explanation.isEmpty)
  }

  @Test func awaitingWorkItemDetailCarriesPlansApprovalsPullAndDiff() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    #expect(detail.workItem.id == "WI-0020")
    #expect(detail.workItem.state == .awaitingApproval)
    #expect(detail.workItem.riskTier == .t2)
    #expect(detail.workItem.explanation.source == "anthropic/claude-haiku-5.5")
    #expect(detail.workItem.explanation.preChecks.count == 3)
    #expect(detail.plans.count == 2)
    #expect(detail.currentPlans.map(\.id) == [17])
    #expect(detail.currentPlans.first?.facts.resources.first?.details == [
      "values.api.replicas: 1 → 2"
    ])
    #expect(detail.approversRequired == 1)
    #expect(detail.pull?.id == 6)
    #expect(detail.diff.contains("+api_replicas      = 2"))
    #expect(detail.events.contains { $0.type == "apply.refused" })
    #expect(detail.grants.count == 1)
  }

  @Test func auditEventsDecodeFreeFormDataAndMissingOptionalFields() throws {
    let events = try Fixture.decode([AuditEvent].self, "events")
    #expect(events.count == 5)
    let first = try #require(events.first)
    // `env`, `layer` and `work_item` are omitted when empty.
    #expect(first.workItem.isEmpty)
    #expect(first.layer.isEmpty)
    #expect(first.string("mode") == "openrouter")
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    let created = try #require(detail.events.first { $0.type == "plan.created" })
    #expect(created.data["change"]?.intValue == 1)
    #expect(created.data["job"]?.intValue == 39)
    let grant = try #require(detail.events.first { $0.type == "grant.issued" })
    #expect(grant.data["approvers"]?.stringArray == ["alice"])
  }

  @Test func usersAccessChatAndPaperboatDecode() throws {
    let users = try Fixture.decode([User].self, "users")
    #expect(users.map(\.name) == ["alice", "bob", "carol", "dave"])
    #expect(users.filter(\.canDecide).map(\.name) == ["alice", "bob", "carol"])
    let access = try Fixture.decode(AccessReport.self, "access")
    #expect(access.identities.contains { $0.name == "applier-50-app" })
    #expect(access.active.isEmpty)
    #expect(access.history.count > 20)
    let chat = try Fixture.decode([ChatEntry].self, "chat")
    #expect(chat.last?.isAssistant == true)
    let stats = try Fixture.decode(PaperboatStats.self, "paperboat_stats")
    #expect(stats.fishAvailable == 45)
    #expect(stats.rentalsActive == 10)
    #expect(stats.ticketsOpen == 12)
    #expect(try Fixture.decode(PaperboatVersion.self, "paperboat_version").version == "1.1.1")
    #expect(try Fixture.decode(PaperboatHealth.self, "paperboat_healthz").isOK)
  }

  @Test func unknownFieldsNullListsAndMissingOptionalsAreTolerated() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    #expect(detail.workItem.id == "WI-9001")
    #expect(detail.grants.isEmpty)
    #expect(detail.pull == nil)
    #expect(detail.diff.isEmpty)
    let minimal = try Fixture.decode(WorkItem.self, json: #"{"id": "WI-1"}"#)
    #expect(minimal.title.isEmpty)
    #expect(minimal.state == .unknown(""))
  }

  @Test func aStateThisAppDoesNotKnowStaysReadable() throws {
    let item = try Fixture.decode(WorkItem.self, json: #"{"id": "WI-1", "state": "Paused"}"#)
    #expect(item.state == .unknown("Paused"))
    #expect(item.state.label == "Paused")
    #expect(!item.state.isFinal)
  }

  @Test func aWorkItemWithoutIDIsMalformed() {
    #expect(throws: DecodingError.self) {
      try Fixture.decode(WorkItem.self, json: #"{"title": "no id"}"#)
    }
  }

  @Test func aFieldOfTheWrongTypeIsMalformed() {
    #expect(throws: DecodingError.self) {
      try Fixture.decode(WorkItem.self, json: #"{"id": "WI-1", "layers": "50-app"}"#)
    }
  }
}

@Suite("State and tier mapping")
struct StateMappingTests {
  @Test(arguments: WorkState.known.map(\.name))
  func everyDocumentedStateIsKnown(name: String) {
    let state = WorkState(name: name)
    #expect(state != .unknown(name))
    #expect(!state.label.isEmpty)
    #expect(!state.symbol.isEmpty)
  }

  @Test func finalAndRunningStates() {
    #expect(WorkState.done.isFinal)
    #expect(WorkState.rolledBack.isFinal)
    #expect(!WorkState.awaitingApproval.isFinal)
    #expect(WorkState.applying.isRunning)
    #expect(!WorkState.scheduled.isRunning)
    #expect(WorkState.failed.tone == .bad)
    #expect(WorkState.awaitingApproval.tone == .attention)
  }

  @Test func tiers() {
    #expect(RiskTier(name: "T3") == .t3)
    #expect(RiskTier(name: "") == .none)
    #expect(RiskTier(name: "T9") == .unknown("T9"))
    #expect(RiskTier.t2.approvalRule == "Needs one approver.")
    #expect(RiskTier.t3.approvalRule.contains("one or two approvers"))
  }
}
