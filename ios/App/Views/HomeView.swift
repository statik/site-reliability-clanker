import Charts
import SwiftUI

struct HomeView: View {
  @Environment(AppModel.self) private var app
  @State private var model = HomeModel()

  var body: some View {
    ScrollViewReader { proxy in
      content
        .task(id: app.generation) {
          await model.load(app: app)
          if !app.options.scrollTo.isEmpty {
            proxy.scrollTo(app.options.scrollTo, anchor: .top)
          }
        }
    }
    .navigationTitle("Paperboat")
    .toolbar {
      ToolbarItem(placement: .topBarTrailing) { AccountButton() }
    }
    .refreshable { await model.load(app: app) }
    .onChange(of: app.latestEvent?.seq) {
      guard model.consume(app.recentEvents) else { return }
      Task { await model.load(app: app) }
    }
  }

  private var content: some View {
    List {
      if let error = model.controlPlaneError {
        Section("Clanker control plane") {
          ErrorNotice(message: error) { Task { await model.load(app: app) } }
        }
      }
      if let overview = model.overview {
        WaitingSection(items: overview.waiting)
      }
      BusinessSection(business: model.business, error: model.businessError)
      if let overview = model.overview {
        ForEach(overview.environments) { environment in
          EnvironmentSection(
            environment: environment, history: model.healthHistory[environment.name] ?? []
          )
          .id("environment")
        }
        CatchUpSection(catchUp: overview.catchUp).id("catchup")
        ChainSection(chain: overview.chain, stream: app.streamStatus).id("chain")
      } else if model.controlPlaneError == nil {
        Section { ProgressView("Loading the overview") }
      }
    }
  }
}

private struct WaitingSection: View {
  let items: [WorkItem]

  var body: some View {
    Section {
      if items.isEmpty {
        Label("Nothing waits for your decision", systemImage: "checkmark.circle")
          .foregroundStyle(.secondary)
      }
      ForEach(items) { item in
        NavigationLink(value: Route.approval(item.id)) {
          WaitingRow(item: item)
        }
      }
    } header: {
      Label("Needs your decision", systemImage: "hand.raised.fill")
        .font(.headline)
        .foregroundStyle(items.isEmpty ? Color.secondary : Color.orange)
        .textCase(nil)
    }
  }
}

private struct WaitingRow: View {
  let item: WorkItem

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text(item.title)
        .font(.body.weight(.semibold))
      TierBadge(tier: item.riskTier, showsMeaning: true)
      if !item.summary.isEmpty {
        Text(item.summary)
          .font(.subheadline)
          .foregroundStyle(.secondary)
          .lineLimit(3)
      }
      Text("\(item.id) · \(item.env) · \(item.trigger)")
        .font(.caption)
        .foregroundStyle(.secondary)
    }
    .accessibilityElement(children: .combine)
    .accessibilityHint("Opens the approval page")
  }
}

private struct BusinessSection: View {
  let business: BusinessStatus?
  let error: String?

  var body: some View {
    Section {
      if let error {
        ErrorNotice(message: error)
      } else if let business {
        AdaptiveRow {
          StatusLabel(
            text: business.health.isOK ? "Healthy" : "Unhealthy: \(business.health.status)",
            symbol: business.health.isOK ? "heart.fill" : "heart.slash.fill",
            tone: business.health.isOK ? .good : .bad)
        } trailing: {
          Text("Version \(business.version.version)")
            .font(.subheadline.monospaced())
            .foregroundStyle(.secondary)
        }
        StatGrid(stats: business.stats)
      } else {
        ProgressView("Asking Paperboat")
      }
    } header: {
      Text("The business: Paperboat fish rental")
    } footer: {
      if let stats = business?.stats {
        let p95 = stats.p95MS.formatted(.number.precision(.fractionLength(1)))
        Text(
          "Reported by Paperboat itself: \(stats.requests) requests, \(stats.errors) errors, "
            + "95% answered within \(p95) ms.")
      }
    }
  }
}

private struct StatGrid: View {
  let stats: PaperboatStats
  @Environment(\.dynamicTypeSize) private var typeSize

  var body: some View {
    let columns = [GridItem(.adaptive(minimum: typeSize.isAccessibilitySize ? 240 : 130))]
    LazyVGrid(columns: columns, spacing: 12) {
      StatTile(value: stats.fishAvailable, label: "Fish available", symbol: "fish.fill")
      StatTile(value: stats.rentalsActive, label: "Rentals active", symbol: "calendar")
      StatTile(value: stats.deliveriesOut, label: "Deliveries out", symbol: "shippingbox.fill")
      StatTile(value: stats.ticketsOpen, label: "Tickets open", symbol: "lifepreserver.fill")
    }
    .padding(.vertical, 4)
  }
}

private struct StatTile: View {
  let value: Int
  let label: String
  let symbol: String

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Label(label, systemImage: symbol)
        .font(.caption)
        .foregroundStyle(.secondary)
      Text(value, format: .number)
        .font(.title2.weight(.semibold).monospacedDigit())
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .padding(10)
    .background(.fill.tertiary, in: RoundedRectangle(cornerRadius: 10))
    .accessibilityElement(children: .ignore)
    .accessibilityLabel("\(label): \(value)")
  }
}

private struct EnvironmentSection: View {
  let environment: EnvironmentStatus
  let history: [HealthSample]

  var body: some View {
    Section {
      HStack {
        healthLabel
        Spacer()
        if let health = environment.health {
          Text("p95 \(health.p95MS.formatted(.number.precision(.fractionLength(1)))) ms")
            .font(.subheadline.monospacedDigit())
            .foregroundStyle(.secondary)
        }
      }
      if history.count > 1 {
        HealthChart(samples: history)
      }
      ForEach(versionRows, id: \.name) { row in
        LabeledContent(row.name) {
          Text(row.version.isEmpty ? "not deployed" : row.version)
            .font(.callout.monospaced())
        }
      }
      if environment.openWorkItems > 0 {
        LabeledContent("Open work items", value: "\(environment.openWorkItems)")
      }
    } header: {
      Text("Environment: \(environment.name) (\(environment.cloud))")
    }
  }

  private var healthLabel: StatusLabel {
    guard environment.deployed else {
      return StatusLabel(text: "Not deployed", symbol: "shippingbox", tone: .neutral)
    }
    guard let health = environment.health, health.total > 0 else {
      return StatusLabel(text: "No health data", symbol: "questionmark.circle", tone: .neutral)
    }
    let text = "\(health.ok) of \(health.total) probes pass"
    if health.ok == health.total {
      return StatusLabel(text: text, symbol: "checkmark.circle.fill", tone: .good)
    }
    let degraded = health.successRate >= 0.9
    return StatusLabel(
      text: text, symbol: degraded ? "exclamationmark.triangle.fill" : "xmark.octagon.fill",
      tone: degraded ? .attention : .bad)
  }

  private var versionRows: [(name: String, version: String)] {
    let names = [
      ("paperboat", "Paperboat"), ("postgres", "Postgres"), ("kubernetes", "Kubernetes"),
      ("node_image", "Node image"),
    ]
    return names.map { (name: $0.1, version: environment.versions[$0.0] ?? "") }
  }
}

private struct HealthChart: View {
  let samples: [HealthSample]

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text("Probe success, last \(samples.count) samples")
        .font(.caption)
        .foregroundStyle(.secondary)
      Chart(Array(samples.enumerated()), id: \.offset) { index, sample in
        BarMark(
          x: .value("Sample", index), y: .value("Success", sample.successRate * 100), width: 3
        )
        .foregroundStyle(sample.ok == sample.total ? Color.green : Color.red)
      }
      .chartYScale(domain: 0...100)
      .chartXAxis(.hidden)
      .chartYAxis {
        AxisMarks(values: [0, 50, 100]) { value in
          AxisGridLine()
          AxisValueLabel {
            if let percent = value.as(Int.self) { Text("\(percent)%") }
          }
        }
      }
      .frame(height: 72)
    }
    .accessibilityElement(children: .ignore)
    .accessibilityLabel(summary)
  }

  private var summary: String {
    let failing = samples.filter { $0.ok < $0.total }.count
    return "Probe success chart: \(failing) of the last \(samples.count) samples had failures"
  }
}

private struct CatchUpSection: View {
  let catchUp: CatchUp

  var body: some View {
    Section {
      Text(catchUp.narrative)
        .font(.callout)
      SourceNote(origin: .auditLog)
    } header: {
      Text("Since \(Timestamps.display(catchUp.since))")
    } footer: {
      if !catchUp.narrativeSource.isEmpty {
        Text("Source: \(catchUp.narrativeSource).")
      }
    }
  }
}

private struct ChainSection: View {
  let chain: ChainStatus
  let stream: StreamStatus

  var body: some View {
    Section {
      StatusLabel(
        text: chain.ok
          ? "Chain intact: \(chain.count) events verified"
          : "Chain BROKEN at event \(chain.brokenSeq)",
        symbol: chain.ok ? "checkmark.shield.fill" : "exclamationmark.shield.fill",
        tone: chain.ok ? .good : .bad)
      if !chain.reason.isEmpty {
        Text(chain.reason)
          .font(.callout)
      }
      FieldRow(label: "Head hash", value: chain.headHash.shortHash + "…", monospaced: true)
      StatusLabel(
        text: "Event stream: \(stream.label)", symbol: stream.symbol,
        tone: stream == .connected ? .good : .attention)
    } header: {
      Text("Audit log")
    } footer: {
      Text("Every event is hashed together with the event before it, so a later edit shows.")
    }
  }
}
