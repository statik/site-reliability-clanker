import SwiftUI

/// The approval page of section 7.2 of the plan, for a phone. The order of the sections is
/// the order of that page: sentence, impact, tier, changes, safety, permission, evidence,
/// ask, decide.
struct ApprovalDetailView: View {
  @Environment(AppModel.self) private var app
  @State private var model: WorkItemDetailModel
  @State private var sheetChoice: DecisionSheetStart?
  @State private var didApplyLaunchOptions = false

  init(id: String) {
    _model = State(initialValue: WorkItemDetailModel(id: id))
  }

  var body: some View {
    ScrollViewReader { proxy in
      List {
        if let error = model.error {
          Section { ErrorNotice(message: error) { Task { await model.load(app: app) } } }
        }
        if let detail = model.detail {
          sections(detail)
        }
      }
      .task(id: app.generation) {
        await model.load(app: app)
        applyLaunchOptions(proxy)
      }
    }
    .navigationTitle(model.id)
    .navigationBarTitleDisplayMode(.inline)
    .toolbar(.hidden, for: .tabBar)
    .overlay {
      if model.detail == nil && model.error == nil { ProgressView() }
    }
    .safeAreaInset(edge: .bottom) { decideBar }
    .refreshable { await model.load(app: app) }
    .onChange(of: app.latestEvent?.seq) {
      guard model.consume(app.recentEvents) else { return }
      Task { await model.load(app: app) }
    }
    .sheet(item: $sheetChoice) { start in
      DecisionSheet(model: model, start: start.choice, autoSubmit: start.autoSubmit)
    }
  }

  @ViewBuilder
  private func sections(_ detail: WorkItemDetail) -> some View {
    let item = detail.workItem
    SummarySection(item: item).id("summary")
    if item.state != .awaitingApproval {
      StateNoticeSection(item: item)
    }
    ImpactSection(explanation: item.explanation).id("impact")
    TierSection(item: item, plans: detail.currentPlans).id("tier")
    ChangesSection(changes: model.changes).id("changes")
    SafetySection(explanation: item.explanation).id("safety")
    PermissionSection(permission: model.permission, accessError: model.accessError)
      .id("permission")
    EvidenceSection(detail: detail).id("evidence")
    Section("Ask about this change") {
      NavigationLink(value: Route.chat(item.id)) {
        Label("Ask Clanker about \(item.id)", systemImage: "bubble.left.and.text.bubble.right")
      }
    }
    .id("ask")
    ApproversSection(progress: model.approvers, users: app.users).id("approvers")
    if model.progress.hasStarted {
      ProgressSections(progress: model.progress, state: item.state).id("progress")
    }
  }

  @ViewBuilder
  private var decideBar: some View {
    if let detail = model.detail, detail.workItem.state == .awaitingApproval {
      VStack(spacing: 6) {
        Button {
          sheetChoice = DecisionSheetStart(choice: nil)
        } label: {
          Label("Decide", systemImage: "hand.raised.fill")
            .font(.headline)
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .accessibilityIdentifier("decide-button")
        Text("\(model.approvers.headline) so far. Approval releases the apply.")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
      .padding(.horizontal)
      .padding(.vertical, 10)
      .background(.bar)
    }
  }

  /// Honours `-debugScroll` and `-debugDecision` once, for screenshots without taps.
  private func applyLaunchOptions(_ proxy: ScrollViewProxy) {
    guard !didApplyLaunchOptions, model.detail != nil else { return }
    didApplyLaunchOptions = true
    let options = app.options
    if !options.scrollTo.isEmpty {
      proxy.scrollTo(options.scrollTo, anchor: .top)
    }
    if options.decision == "choose" {
      sheetChoice = DecisionSheetStart(choice: nil)
    } else if let choice = DecisionChoice(rawValue: options.decision) {
      sheetChoice = DecisionSheetStart(choice: choice, autoSubmit: options.submitDecision)
    }
  }
}

struct DecisionSheetStart: Identifiable {
  let id = UUID()
  var choice: DecisionChoice?
  var autoSubmit = false
}

private struct SummarySection: View {
  let item: WorkItem

  var body: some View {
    Section {
      VStack(alignment: .leading, spacing: 8) {
        AdaptiveRow {
          StatusLabel(state: item.state)
        } trailing: {
          TierBadge(tier: item.riskTier, showsMeaning: true)
        }
        Text(item.title)
          .font(.title3.weight(.semibold))
        Text("\(Humanise.kind(item.kind)) in \(item.env)")
          .font(.subheadline)
          .foregroundStyle(.secondary)
      }
      if !item.explanation.sentence.isEmpty {
        Text(item.explanation.sentence)
          .font(.body)
          .accessibilityIdentifier("summary-sentence")
      }
      if !item.explanation.why.isEmpty {
        FieldRow(label: "Why now", value: item.explanation.why)
      }
      if !item.explanation.isEmpty {
        SourceNote(origin: .model(source: item.explanation.source))
      }
    } header: {
      Text("1 · In one sentence")
    }
  }
}

private struct StateNoticeSection: View {
  let item: WorkItem

  var body: some View {
    Section {
      Label(text, systemImage: "info.circle")
        .font(.callout)
    }
  }

  private var text: String {
    switch item.state {
    case .scheduled:
      "Approved. The apply waits for the window at \(Timestamps.display(item.scheduledFor))."
    case .approved, .applying, .verifying:
      "Approved. The apply was released; progress is at the end of this page."
    default:
      "This work item is \(item.state.label.lowercased()). It does not wait for a decision."
    }
  }
}

private struct ImpactSection: View {
  let explanation: Explanation

  var body: some View {
    Section {
      FieldRow(label: "Downtime", value: explanation.downtime)
      FieldRow(label: "Effect on users", value: explanation.userEffect)
      FieldRow(label: "Cost", value: explanation.costChange)
      FieldRow(label: "How long it takes", value: explanation.duration)
      SourceNote(origin: .model(source: explanation.source))
    } header: {
      Text("2 · Impact")
    }
  }
}

private struct TierSection: View {
  let item: WorkItem
  let plans: [Plan]

  var body: some View {
    Section {
      HStack {
        TierBadge(tier: item.riskTier, showsMeaning: true)
        Spacer()
      }
      Text(item.riskTier.approvalRule)
        .font(.callout)
      FieldRow(label: "Rule that set the tier", value: item.tierRule, monospaced: true)
      ForEach(reasons, id: \.self) { reason in
        Label(reason, systemImage: "arrow.turn.down.right")
          .font(.callout)
      }
      SourceNote(origin: .policy)
    } header: {
      Text("3 · Risk tier")
    }
  }

  /// The reasons of every current plan, each one once, in plan order.
  private var reasons: [String] {
    var seen: Set<String> = []
    return plans.flatMap(\.policy.reasons).filter { seen.insert($0).inserted }
  }
}
