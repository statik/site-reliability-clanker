import SwiftUI

/// The work items that wait for a human decision.
struct ApprovalsView: View {
  @Environment(AppModel.self) private var app
  @State private var model = WorkItemsModel()

  var body: some View {
    PaletteList {
      if let error = model.error {
        Section { ErrorNotice(message: error) { Task { await model.load(app: app) } } }
      }
      if model.hasLoaded && model.error == nil && model.awaiting.isEmpty {
        Section {
          EmptyState(
            title: "Nothing to decide",
            message: "No change waits for a decision. S.R.C. will put new proposals here.")
        }
      }
      if !model.awaiting.isEmpty {
        Section {
          ForEach(model.awaiting) { item in
            NavigationLink(value: Route.approval(item.id)) {
              ApprovalRow(item: item)
            }
          }
        } header: {
          Text("Needs your decision")
        } footer: {
          Text(
            "Approval is what releases the apply. Each approval is bound to the exact commit "
              + "and plan hash that you saw.")
        }
      }
    }
    .navigationTitle("Approvals")
    .toolbar {
      ToolbarItem(placement: .topBarTrailing) { AccountButton() }
    }
    .overlay {
      if !model.hasLoaded { ProgressView() }
    }
    .refreshable { await model.load(app: app) }
    .task(id: app.generation) { await model.load(app: app) }
    .onChange(of: app.latestEvent?.seq) {
      guard model.consume(app.recentEvents) else { return }
      Task { await model.load(app: app) }
    }
  }
}

private struct ApprovalRow: View {
  let item: WorkItem

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      HStack {
        Text(item.id)
          .font(.caption.monospaced())
          .foregroundStyle(Palette.textSecondary)
        Spacer()
        TierBadge(tier: item.riskTier, showsMeaning: true)
      }
      Text(item.title)
        .font(.headline)
      if !item.summary.isEmpty {
        Text(item.summary)
          .font(.subheadline)
          .foregroundStyle(Palette.textSecondary)
          .lineLimit(4)
      }
      Text("\(item.env) · \(item.layers.map(Humanise.layer).joined(separator: ", "))")
        .font(.caption)
        .foregroundStyle(Palette.textSecondary)
    }
    .padding(.vertical, 2)
    .accessibilityElement(children: .combine)
    .accessibilityHint("Opens the approval page")
  }
}
