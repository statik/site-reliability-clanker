import SwiftUI

/// Every work item, with a filter on the state.
struct ActivityView: View {
  @Environment(AppModel.self) private var app
  @State private var model = WorkItemsModel()

  var body: some View {
    PaletteList {
      if let error = model.error {
        Section { ErrorNotice(message: error) { Task { await model.load(app: app) } } }
      }
      if !model.items.isEmpty {
        Section {
          StateFilterBar(counts: model.stateCounts, total: model.items.count,
            selection: $model.stateFilter)
        }
        .listRowInsets(EdgeInsets())
        .listRowBackground(Color.clear)
      }
      Section {
        ForEach(model.filtered) { item in
          NavigationLink(value: Route.activity(item.id)) {
            WorkItemRow(item: item)
          }
        }
      }
      if model.hasLoaded && model.error == nil && model.filtered.isEmpty {
        Section {
          EmptyState(
            title: "No work items",
            message: "Nothing matches. Work that S.R.C. detects or proposes shows up here.")
        }
      }
    }
    .navigationTitle("Activity")
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

private struct StateFilterBar: View {
  let counts: [StateCount]
  let total: Int
  @Binding var selection: String?

  var body: some View {
    ScrollView(.horizontal, showsIndicators: false) {
      HStack(spacing: 8) {
        chip(title: "All", symbol: "tray.full", count: total, value: nil)
        ForEach(counts) { entry in
          chip(
            title: entry.state.label, symbol: entry.state.symbol, count: entry.count,
            value: entry.name)
        }
      }
      .padding(.horizontal, 4)
      .padding(.vertical, 2)
    }
  }

  private func chip(title: String, symbol: String, count: Int, value: String?) -> some View {
    let isSelected = selection == value
    return Button {
      selection = value
    } label: {
      Label("\(title) \(count)", systemImage: symbol)
        .font(.subheadline)
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .foregroundStyle(isSelected ? Palette.textOnInteractive : Palette.text)
        .background(isSelected ? Palette.interactive : Palette.layerRaised, in: Capsule())
    }
    .buttonStyle(.plain)
    .accessibilityLabel("\(title), \(count) work items")
    .accessibilityAddTraits(isSelected ? .isSelected : [])
  }
}

/// One work item in Activity: where it stands, its timeline and its jobs with logs.
struct ActivityDetailView: View {
  @Environment(AppModel.self) private var app
  @State private var model: WorkItemDetailModel

  init(id: String) {
    _model = State(initialValue: WorkItemDetailModel(id: id))
  }

  var body: some View {
    ScrollViewReader { proxy in
      PaletteList {
        if let error = model.error {
          Section { ErrorNotice(message: error) { Task { await model.load(app: app) } } }
        }
        if let detail = model.detail {
          header(detail)
          if model.progress.hasStarted {
            ProgressSections(progress: model.progress, state: detail.workItem.state)
              .id("progress")
          }
          JobsSection(jobs: detail.jobs).id("jobs")
          TimelineSection(events: detail.events).id("timeline")
        }
      }
      .task(id: app.generation) {
        await model.load(app: app)
        if !app.options.scrollTo.isEmpty {
          proxy.scrollTo(app.options.scrollTo, anchor: .top)
        }
      }
    }
    .navigationTitle(model.id)
    .navigationBarTitleDisplayMode(.inline)
    .overlay {
      if model.detail == nil && model.error == nil { ProgressView() }
    }
    .refreshable { await model.load(app: app) }
    .onChange(of: app.latestEvent?.seq) {
      guard model.consume(app.recentEvents) else { return }
      Task { await model.load(app: app) }
    }
  }

  @ViewBuilder
  private func header(_ detail: WorkItemDetail) -> some View {
    let item = detail.workItem
    Section {
      WorkItemRow(item: item)
      if !item.summary.isEmpty {
        Text(item.summary).font(.callout)
        SourceNote(origin: .model(source: item.explanation.source))
      }
      FieldRow(label: "Trigger", value: item.trigger)
      if !item.rollbackOf.isEmpty {
        NavigationLink(value: Route.activity(item.rollbackOf)) {
          Label("Rolls back \(item.rollbackOf)", systemImage: "arrow.counterclockwise")
        }
      }
      if !detail.plans.isEmpty {
        NavigationLink(value: Route.approval(item.id)) {
          Label("Approval page: impact, changes, safety", systemImage: "doc.text")
        }
      }
      NavigationLink(value: Route.chat(item.id)) {
        Label("Ask S.R.C. about \(item.id)", systemImage: "bubble.left.and.text.bubble.right")
      }
    }
  }
}

private struct JobsSection: View {
  let jobs: [Job]

  var body: some View {
    if !jobs.isEmpty {
      Section("Jobs") {
        ForEach(jobs) { job in
          NavigationLink(value: Route.jobLog(job.id)) {
            VStack(alignment: .leading, spacing: 2) {
              AdaptiveRow {
                Text("Job \(job.id): \(job.kind) \(job.layer)")
                  .font(.callout)
              } trailing: {
                StatusLabel(
                  text: job.status.capitalized, symbol: symbol(job.status), tone: tone(job.status))
              }
              Text(Timestamps.display(job.startedAt))
                .font(.caption)
                .foregroundStyle(Palette.textSecondary)
            }
            .accessibilityElement(children: .combine)
          }
        }
      }
    }
  }

  private func symbol(_ status: String) -> String {
    switch status {
    case "succeeded": "checkmark.circle.fill"
    case "failed", "lost": "xmark.octagon.fill"
    default: "hourglass"
    }
  }

  private func tone(_ status: String) -> Tone {
    switch status {
    case "succeeded": .good
    case "failed", "lost": .bad
    default: .active
    }
  }
}

private struct TimelineSection: View {
  let events: [AuditEvent]

  var body: some View {
    Section {
      ForEach(events.sorted { $0.seq > $1.seq }) { event in
        TimelineRow(event: event)
      }
    } header: {
      Text("Timeline (\(events.count) audit events, newest first)")
    }
  }
}

private struct TimelineRow: View {
  let event: AuditEvent

  var body: some View {
    let step = ApplyProgress.step(for: event)
    HStack(alignment: .top, spacing: 10) {
      Image(systemName: step.symbol)
        .foregroundStyle(step.tone.color)
        .frame(width: 22)
        .accessibilityHidden(true)
      VStack(alignment: .leading, spacing: 2) {
        Text(event.summary)
          .font(.callout)
        Text("\(event.type) · \(event.actor)\(event.layer.isEmpty ? "" : " · \(event.layer)")")
          .font(.caption.monospaced())
          .foregroundStyle(Palette.textSecondary)
        Text("#\(event.seq) · \(Timestamps.display(event.ts))")
          .font(.caption2.monospacedDigit())
          .foregroundStyle(Palette.textHelper)
      }
    }
    .accessibilityElement(children: .combine)
  }
}

/// The log of one runner job.
struct JobLogView: View {
  @Environment(AppModel.self) private var app
  let jobID: Int
  @State private var model = JobLogModel()

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 12) {
        if let error = model.error {
          ErrorNotice(message: error) { Task { await model.load(id: jobID, app: app) } }
        }
        if let jobLog = model.jobLog {
          Text("\(jobLog.job.kind) \(jobLog.job.layer) · \(jobLog.job.status)")
            .font(.subheadline.weight(.semibold))
          Text(jobLog.log.isEmpty ? "The log is empty." : jobLog.log)
            .font(.caption.monospaced())
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
        } else if model.error == nil {
          ProgressView()
        }
      }
      .padding()
    }
    .paletteGround()
    .navigationTitle("Job \(jobID)")
    .navigationBarTitleDisplayMode(.inline)
    .task(id: app.generation) { await model.load(id: jobID, app: app) }
  }
}
