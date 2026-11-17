import Foundation
import Testing

@testable import PaperboatManager

@Suite("Approver progress")
struct ApproverProgressTests {
  @Test func anApprovalOfASupersededPlanDoesNotCount() throws {
    // Live case: alice approved WI-0020, the plan went stale and the item was planned again.
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    let progress = ApproverProgress(detail: detail)
    #expect(progress.count == 0)
    #expect(progress.required == 1)
    #expect(progress.headline == "0 of 1 approver")
    #expect(progress.stale.map(\.approver) == ["alice"])
    #expect(!progress.isComplete)
    #expect(!progress.hasValidApproval(from: "alice"))
  }

  @Test func afterTheApplyDeferredLayersDoNotMakeAnApprovalStale() throws {
    // The approvals of the blank slate name only 00-bootstrap; five layers were planned later.
    var detail = try Fixture.decode(WorkItemDetail.self, "work_item_blank_slate")
    #expect(ApproverProgress(detail: detail).stale.isEmpty)
    // While the item waits for approval the hashes must match exactly, as on the server.
    detail.workItem.stateName = "AwaitingApproval"
    #expect(ApproverProgress(detail: detail).count == 0)
  }

  @Test func twoDistinctApproversCompleteATierThreeItem() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_blank_slate")
    let progress = ApproverProgress(detail: detail)
    #expect(progress.required == 2)
    #expect(progress.count == 2)
    #expect(progress.isComplete)
    #expect(progress.headline == "2 of 2 approvers")
    #expect(Set(progress.valid.map(\.approver)).count == 2)
  }

  @Test func theSameApproverCountsOnce() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    let progress = ApproverProgress(detail: detail)
    #expect(progress.count == 1)
    #expect(progress.remaining == 1)
    #expect(progress.stale.isEmpty)
    #expect(progress.hasValidApproval(from: "alice"))
    #expect(!progress.hasValidApproval(from: "bob"))
  }

  @Test func rejectionsAndChangeRequestsAreNotApprovals() throws {
    var detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    detail.approvals[1].decision = "request_changes"
    detail.approvals[1].approver = "bob"
    let progress = ApproverProgress(detail: detail)
    #expect(progress.count == 1)
    #expect(progress.other.map(\.approver) == ["bob"])
  }

  @Test func anItemThatNeedsNoApprovalSaysSo() {
    let progress = ApproverProgress(detail: WorkItemDetail())
    #expect(progress.headline == "No approval is needed")
    #expect(!progress.isComplete)
  }
}

@Suite("What changes")
struct ChangeSummaryTests {
  @Test func supersededPlansAreLeftOut() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    let changes = ChangeSummary(detail: detail)
    #expect(changes.layers.map(\.layer) == ["50-app"])
    #expect(changes.totalText == "1 to change")
    #expect(changes.destructive.isEmpty)
    let resource = try #require(changes.layers.first?.resources.first)
    #expect(resource.title == "Helm release “paperboat”")
    #expect(resource.action == .update)
    #expect(resource.details == ["values › api › replicas: 1 → 2"])
  }

  @Test func layersFollowApplyOrderWithTotals() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_blank_slate")
    let changes = ChangeSummary(detail: detail)
    #expect(changes.layers.map(\.layer) == [
      "00-bootstrap", "10-network", "20-cluster", "30-data", "40-platform", "50-app",
    ])
    #expect(changes.add == 19)
    // Five layers were deferred at approval time and planned during the apply.
    #expect(changes.layers.map(\.plannedAtApply) == [false, true, true, true, true, true])
    #expect(changes.totalText == "19 to create")
    #expect(changes.deferredLayers.isEmpty)
  }

  @Test func destroysAndReplacementsComeFirstAndDeferredLayersAreMarked() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    let changes = ChangeSummary(detail: detail)
    #expect(changes.layers.map(\.layer) == ["30-data", "40-platform", "50-app"])
    #expect(changes.deferredLayers == ["40-platform", "50-app"])
    #expect(changes.layers[1].countsText == "Planned at apply time")
    #expect(changes.totalText == "1 to destroy, 1 to replace, 1 to change, 1 to create")
    #expect(changes.destructive.map(\.action) == [.destroy, .replace])
    #expect(changes.layers[0].resources.map(\.action) == [.destroy, .replace, .update, .create])
    #expect(changes.destructive.first?.title == "Kubernetes persistent volume claim “scratch”")
  }

  @Test func aWorkItemWithoutPlansHasNoLayers() throws {
    let items = try Fixture.decode([WorkItem].self, "work_items")
    var detail = WorkItemDetail()
    detail.workItem = try #require(items.first { $0.id == "WI-0018" })
    let changes = ChangeSummary(detail: detail)
    #expect(changes.layers.isEmpty)
    #expect(!changes.hasPlans)
  }

  @Test func humanisedNames() {
    #expect(Humanise.layer("30-data") == "Data (30-data)")
    #expect(Humanise.layer("60-extra") == "60-extra")
    #expect(Humanise.resource(type: "aws_iam_role", name: "runner") == "Aws IAM role “runner”")
    #expect(Humanise.resource(type: "", name: "") == "Resource")
    #expect(Humanise.detail("no separator") == "no separator")
    #expect(Humanise.counts(add: 0, change: 0, destroy: 0, replace: 0) == "No changes")
    #expect(Humanise.kind("runbook:db-backup") == "Runbook: db-backup")
    #expect(Humanise.kind("app-upgrade") == "App upgrade")
    #expect(Humanise.kind("") == "Work item")
  }
}

@Suite("Apply progress from audit events")
struct ApplyProgressTests {
  @Test func aStaleplanRefusalIsShownAndTheGrantIsRevoked() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    let progress = ApplyProgress(workItem: detail.workItem, events: detail.events)
    #expect(progress.hasStarted)
    #expect(progress.refusal == "the saved plan is stale: tofu apply for local/50-app")
    let layer = try #require(progress.layers.first)
    #expect(layer.layer == "50-app")
    #expect(layer.status == .failed)
    #expect(layer.grant == .revoked(id: "G-ecd14c90d731"))
    #expect(layer.jobID == 42)
    #expect(progress.verification == .pending)
    // The run starts with the decision that released it and ends back at approval.
    #expect(progress.steps.first?.title == "Decision by alice: approve")
    #expect(progress.steps.last?.title == "Planned → Needs decision")
  }

  @Test func aFailedVerificationCarriesItsReasonAndChecks() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_rolled_back")
    let progress = ApplyProgress(workItem: detail.workItem, events: detail.events)
    #expect(progress.layers.map(\.status) == [.applied])
    guard case .failed(let reason, let checks) = progress.verification else {
      Issue.record("Expected a failed verification, got \(progress.verification)")
      return
    }
    #expect(reason.hasPrefix("canary failed"))
    #expect(checks.count == 3)
    #expect(progress.refusal.isEmpty)
  }

  @Test func everyLayerOfTheBlankSlateIsAppliedWithItsGrantRevoked() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_blank_slate")
    let progress = ApplyProgress(workItem: detail.workItem, events: detail.events)
    #expect(progress.layers.count == 6)
    #expect(progress.layers.allSatisfy { $0.status == .applied })
    for layer in progress.layers {
      guard case .revoked = layer.grant else {
        Issue.record("The grant of \(layer.layer) is \(layer.grant), not revoked")
        continue
      }
    }
    guard case .passed = progress.verification else {
      Issue.record("Expected a passed verification, got \(progress.verification)")
      return
    }
  }

  @Test func beforeAnyApplyEveryLayerWaits() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    let progress = ApplyProgress(workItem: detail.workItem, events: detail.events)
    #expect(!progress.hasStarted)
    #expect(progress.layers.map(\.status) == [.waiting, .waiting, .waiting])
    #expect(progress.steps.isEmpty)
  }

  @Test func liveEventsMoveALayerThroughGrantApplyAndRevoke() throws {
    var item = WorkItem()
    item.id = "WI-1"
    item.layers = ["50-app"]
    func event(_ seq: Int, _ type: String, layer: String = "", _ data: String = "{}") throws
      -> AuditEvent
    {
      try Fixture.decode(
        AuditEvent.self,
        json: """
          {"seq": \(seq), "type": "\(type)", "layer": "\(layer)", "work_item": "WI-1",
           "summary": "\(type)", "data": \(data)}
          """)
    }
    var events = [
      try event(1, "approval.recorded", #"{"decision": "approve"}"#),
      try event(2, "workitem.state_changed", #"{"from": "Approved", "to": "Applying"}"#),
      try event(3, "grant.issued", layer: "50-app", #"{"grant": "G-1", "expires": "x"}"#),
    ]
    var progress = ApplyProgress(workItem: item, events: events)
    #expect(progress.layers.first?.status == .grantIssued)
    #expect(progress.layers.first?.grant == .issued(id: "G-1", expires: "x"))
    events.append(try event(4, "apply.started", layer: "50-app", #"{"grant": "G-1"}"#))
    progress = ApplyProgress(workItem: item, events: events)
    #expect(progress.layers.first?.status == .applying)
    events.append(try event(5, "grant.revoked", layer: "50-app", #"{"grant": "G-1"}"#))
    events.append(try event(6, "apply.succeeded", layer: "50-app", #"{"job": 7}"#))
    events.append(try event(7, "verify.passed", #"{"checks": ["health check answers 200"]}"#))
    // Events may arrive out of order after a reconnect.
    progress = ApplyProgress(workItem: item, events: events.reversed())
    #expect(progress.layers.first?.status == .applied)
    #expect(progress.layers.first?.grant == .revoked(id: "G-1"))
    #expect(progress.layers.first?.jobID == 7)
    #expect(progress.verification == .passed(checks: ["health check answers 200"]))
    #expect(progress.steps.map(\.id) == [1, 2, 3, 4, 5, 6, 7])
  }
}

@Suite("Lists and grouping")
struct WorkItemsModelTests {
  @Test func awaitingItemsAndStateFilter() throws {
    let items = try Fixture.decode([WorkItem].self, "work_items")
    #expect(WorkItemsModel.awaitingApproval(items).map(\.id) == ["WI-0020"])
    #expect(WorkItemsModel.filter(items, state: nil).count == 21)
    #expect(WorkItemsModel.filter(items, state: "Failed").count == 7)
    #expect(WorkItemsModel.filter(items, state: "Scheduled").isEmpty)
  }

  @Test func statesAreCountedInLifecycleOrder() throws {
    let items = try Fixture.decode([WorkItem].self, "work_items")
    let counts = WorkItemsModel.stateCounts(items)
    #expect(counts.map(\.name) == ["AwaitingApproval", "Done", "Failed", "RolledBack"])
    #expect(counts.map(\.count) == [1, 12, 7, 1])
    #expect(WorkItemsModel.stateCounts([]).isEmpty)
  }

  @Test func unknownStatesComeLast() throws {
    let items = try Fixture.decode(
      [WorkItem].self,
      json: #"[{"id": "A", "state": "Paused"}, {"id": "B", "state": "Done"}]"#)
    #expect(WorkItemsModel.stateCounts(items).map(\.name) == ["Done", "Paused"])
  }

  @Test func recentGrantsAreNewestFirst() throws {
    let access = try Fixture.decode(AccessReport.self, "access")
    let recent = AccessModel.recent(access.history, limit: 5)
    #expect(recent.count == 5)
    #expect(recent.map(\.issuedAt) == recent.map(\.issuedAt).sorted(by: >))
  }
}

@Suite("Decision confirmation and schedule")
struct DecisionSummaryTests {
  private func summary(
    _ choice: DecisionChoice, fixture: String, user: String, at date: Date? = nil
  ) throws -> DecisionSummary {
    let detail = try Fixture.decode(WorkItemDetail.self, fixture)
    return DecisionSummary(
      choice: choice, detail: detail, changes: ChangeSummary(detail: detail),
      approvers: ApproverProgress(detail: detail), user: user, scheduledFor: date)
  }

  @Test func theLastApprovalSaysThatTheApplyStartsAtOnce() throws {
    let text = try summary(.approve, fixture: "work_item_awaiting", user: "bob")
    #expect(text.headline == "Approve the tier T2 (normal change) change “Run two api replicas” "
      + "in local.")
    #expect(text.runs == ["50-app: 1 to change, commit 2b8921996a…, plan a3ddadf301…"])
    #expect(text.consequence.contains("no separate apply step"))
    #expect(text.consequence.hasSuffix("starting at once."))
  }

  @Test func aScheduledApprovalRestatesTheStartInUTC() throws {
    let date = try #require(Timestamps.parse("2026-12-17T03:00:00.000Z"))
    let text = try summary(.schedule, fixture: "work_item_awaiting", user: "bob", at: date)
    #expect(text.consequence.hasSuffix("starting at 17 Dec 2026, 03:00 UTC."))
  }

  @Test func theFirstOfTwoApprovalsSaysThatNothingRunsYet() throws {
    var detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    detail.approvals = []
    let text = DecisionSummary(
      choice: .approve, detail: detail, changes: ChangeSummary(detail: detail),
      approvers: ApproverProgress(detail: detail), user: "alice", scheduledFor: nil)
    #expect(text.consequence
      == "This is approval 1 of 2. Nothing runs until 1 more approver approves.")
    #expect(text.runs.count == 3)
    #expect(text.runs[1] == "40-platform: planned at apply time; may only create resources")
  }

  @Test func aScheduleOnANonFinalApprovalSaysThatTheTimeIsNotKept() throws {
    var detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    detail.approvals = []
    let date = try #require(Timestamps.parse("2026-12-17T03:00:00.000Z"))
    let text = DecisionSummary(
      choice: .schedule, detail: detail, changes: ChangeSummary(detail: detail),
      approvers: ApproverProgress(detail: detail), user: "alice", scheduledFor: date)
    #expect(text.consequence.hasPrefix("This is approval 1 of 2."))
    #expect(text.consequence.contains("your start time is NOT kept"))
  }

  @Test func approvingTwiceDoesNotCountTwice() throws {
    let text = try summary(.approve, fixture: "work_item_destructive_synthetic", user: "alice")
    #expect(text.consequence.hasPrefix("This is approval 1 of 2."))
    let second = try summary(.approve, fixture: "work_item_destructive_synthetic", user: "bob")
    #expect(second.consequence.contains("(2 of 2)"))
  }

  @Test func rejectAndRequestChangesRunNothing() throws {
    let reject = try summary(.reject, fixture: "work_item_awaiting", user: "bob")
    #expect(reject.runs.isEmpty)
    #expect(reject.consequence.contains("Pull request #6 is closed"))
    let changes = try summary(.requestChanges, fixture: "work_item_awaiting", user: "bob")
    #expect(changes.consequence == "Nothing runs. The work item moves to Changes requested.")
  }

  @Test func theScheduleIsAnchoredOnTheServerClock() throws {
    let now = try #require(Timestamps.parse("2026-12-16T02:27:31.613Z"))
    #expect(Timestamps.format(ScheduleWindow.suggested(serverNow: now))
      == "2026-12-16T03:00:00.000Z")
    #expect(Timestamps.format(ScheduleWindow.earliest(serverNow: now))
      == "2026-12-16T02:29:00.000Z")
    #expect(!ScheduleWindow.isValid(now, serverNow: now))
    #expect(!ScheduleWindow.isValid(now.addingTimeInterval(-3600), serverNow: now))
    #expect(ScheduleWindow.isValid(now.addingTimeInterval(60), serverNow: now))
    let late = try #require(Timestamps.parse("2026-12-16T02:45:00.000Z"))
    #expect(Timestamps.format(ScheduleWindow.suggested(serverNow: late))
      == "2026-12-16T04:00:00.000Z")
  }

  @Test func thePermissionIsStatedForEachLayerThatChanges() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_awaiting")
    let access = try Fixture.decode(AccessReport.self, "access")
    let permission = PermissionSummary(
      detail: detail, changes: ChangeSummary(detail: detail), access: access)
    #expect(permission.lifetime == "30 minutes")
    let line = try #require(permission.lines.first)
    #expect(permission.lines.count == 1)
    #expect(line.identity == "applier-50-app")
    #expect(line.sentence.contains("commit 2b8921996a…"))
    #expect(line.sentence.contains("plan with hash a3ddadf301…"))
    #expect(line.sentence.contains("for at most 30 minutes"))
    #expect(line.brokerRule.contains("one commit and one plan hash"))
  }

  @Test func withoutEarlierGrantsTheLifetimeIsNotInvented() throws {
    let detail = try Fixture.decode(WorkItemDetail.self, "work_item_destructive_synthetic")
    let permission = PermissionSummary(
      detail: detail, changes: ChangeSummary(detail: detail), access: nil)
    #expect(permission.lifetime == nil)
    #expect(permission.lines.count == 3)
    #expect(permission.lines[0].sentence.contains("for a short, fixed time"))
    #expect(permission.lines[1].sentence.contains("may only create resources"))
  }
}

@Suite("Detail model")
@MainActor
struct DetailModelTests {
  private func event(_ seq: Int, _ type: String, workItem: String = "") throws -> AuditEvent {
    try Fixture.decode(
      AuditEvent.self,
      json: #"{"seq": \#(seq), "type": "\#(type)", "work_item": "\#(workItem)"}"#)
  }

  @Test func aBurstThatEndsInAnUnrelatedEventStillReachesTheScreens() throws {
    // Live case: a state change, a grant and a notification arrive in the same instant.
    let burst = [
      try event(10, "workitem.state_changed", workItem: "WI-0020"),
      try event(11, "grant.issued", workItem: "WI-0020"),
      try event(12, "notify.sent"),
    ]
    let detail = WorkItemDetailModel(id: "WI-0020")
    #expect(detail.consume(burst))
    #expect(!detail.consume(burst))
    #expect(WorkItemsModel().consume(burst))
    #expect(HomeModel().consume(burst))
    #expect(AccessModel().consume(burst))
    #expect(!WorkItemDetailModel(id: "WI-0001").consume(burst))
  }

  @Test func theCursorReturnsEachEventOnceInOrder() throws {
    var cursor = EventCursor()
    let first = [try event(2, "a"), try event(1, "b")]
    #expect(cursor.take(from: first).map(\.seq) == [1, 2])
    #expect(cursor.take(from: first).isEmpty)
    #expect(cursor.take(from: first + [try event(3, "c")]).map(\.seq) == [3])
    #expect(cursor.take(from: []).isEmpty)
    #expect(cursor.lastSeq == 3)
  }

  @Test func theAppKeepsABoundedBufferOfStreamEvents() throws {
    let name = "buffer-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: name))
    defer { defaults.removePersistentDomain(forName: name) }
    let app = AppModel(defaults: defaults)
    for seq in 1...(AppModel.eventBufferSize + 5) {
      app.receive(try event(seq, "x"))
    }
    #expect(app.recentEvents.count == AppModel.eventBufferSize)
    #expect(app.recentEvents.first?.seq == 6)
    #expect(app.latestEvent?.seq == AppModel.eventBufferSize + 5)
  }

  @Test func aStreamEventOfAnotherWorkItemIsIgnored() throws {
    let model = WorkItemDetailModel(id: "WI-0020")
    let event = try Fixture.decode(
      AuditEvent.self, json: #"{"seq": 1, "type": "apply.started", "work_item": "WI-0001"}"#)
    #expect(!model.receive(event))
  }

  @Test func theAppDoesNotLoadFromTheNetworkWhileTestsHostIt() {
    #expect(LaunchOptions.read().isUnderTest)
  }

  @Test func launchArgumentsChooseTheScreenAndTheUserWithoutSavingIt() throws {
    let name = "launch-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: name))
    defer { defaults.removePersistentDomain(forName: name) }
    var options = LaunchOptions()
    options.initialScreen = "activity"
    options.workItem = "WI-0005"
    options.user = "alice"
    let app = AppModel(defaults: defaults, options: options)
    #expect(app.selectedTab == .activity)
    #expect(app.path(for: .activity) == [.activity("WI-0005")])
    #expect(app.settings.user == "alice")
    #expect(AppSettings.load(from: defaults).user == "dave")
    var approval = LaunchOptions()
    approval.workItem = "WI-0020"
    let second = AppModel(defaults: defaults, options: approval)
    #expect(second.selectedTab == .approvals)
    #expect(second.path(for: .approvals) == [.approval("WI-0020")])
  }

  @Test func citationsOpenWorkItemsAndGrants() throws {
    let name = "citation-test-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: name))
    defer { defaults.removePersistentDomain(forName: name) }
    let app = AppModel(defaults: defaults)
    app.selectedTab = .ask
    app.open(Citation(type: "work_item", id: "WI-0007", label: "WI-0007"))
    #expect(app.path(for: .ask) == [.activity("WI-0007")])
    app.open(Citation(type: "event", id: "12", label: "event"))
    #expect(app.path(for: .ask).count == 1)
    app.open(Citation(type: "grant", id: "G-1", label: "Grant G-1"))
    #expect(app.selectedTab == .access)
  }
}
