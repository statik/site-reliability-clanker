import SwiftUI

/// Step one of a decision: choose what to do. Step two is `DecisionFormView`.
struct DecisionSheet: View {
  @Environment(\.dismiss) private var dismiss
  let model: WorkItemDetailModel
  @State private var path: [DecisionChoice]
  private let autoSubmit: Bool

  init(model: WorkItemDetailModel, start: DecisionChoice?, autoSubmit: Bool) {
    self.model = model
    self.autoSubmit = autoSubmit
    _path = State(initialValue: start.map { [$0] } ?? [])
  }

  var body: some View {
    NavigationStack(path: $path) {
      List {
        if let item = model.detail?.workItem {
          Section {
            Text(item.title).font(.headline)
            TierBadge(tier: item.riskTier, showsMeaning: true)
            Text(item.riskTier.approvalRule).font(.callout)
          }
          Section {
            ForEach(DecisionChoice.allCases) { choice in
              NavigationLink(value: choice) {
                Label(choice.title, systemImage: choice.symbol)
              }
              .accessibilityIdentifier("choice-\(choice.rawValue)")
            }
          } header: {
            Text("Your decision")
          } footer: {
            Text("The next step shows exactly what will run and asks you to confirm.")
          }
          if !item.explanation.ifDeferred.isEmpty {
            Section {
              Text(item.explanation.ifDeferred).font(.callout)
              SourceNote(origin: .model(source: item.explanation.source))
            } header: {
              Text("If you decide later")
            }
          }
        }
      }
      .navigationTitle("Decide on \(model.id)")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") { dismiss() }
        }
      }
      .navigationDestination(for: DecisionChoice.self) { choice in
        DecisionFormView(model: model, choice: choice, autoSubmit: autoSubmit) { dismiss() }
      }
    }
  }
}

/// Step two: the confirmation. It restates the tier and what will run, takes an optional
/// comment and, for a scheduled approval, the start time.
struct DecisionFormView: View {
  @Environment(AppModel.self) private var app
  let model: WorkItemDetailModel
  let choice: DecisionChoice
  let autoSubmit: Bool
  let onDone: () -> Void

  @State private var comment = ""
  @State private var scheduledFor: Date?
  @State private var clockError: String?

  var body: some View {
    Form {
      if let detail = model.detail {
        ConfirmationSection(summary: summary(detail))
      }
      if choice == .schedule {
        ScheduleSection(
          scheduledFor: $scheduledFor, serverNow: app.serverNow, clockError: clockError)
      }
      Section("Comment (optional)") {
        TextField("Why you decide this way", text: $comment, axis: .vertical)
          .lineLimit(2...5)
          .accessibilityIdentifier("decision-comment")
      }
      if let user = app.currentUser, !user.canDecide {
        Section {
          Label(
            "\(user.display) has the role \(user.role). The control plane accepts decisions "
              + "only from approvers and admins.", systemImage: "person.crop.circle.badge.xmark"
          )
          .font(.callout)
        }
      }
      if let error = model.decisionError {
        Section("The control plane answered") {
          ErrorNotice(message: userMessage(error), status: error.statusLabel)
        }
      }
      Section {
        Button(role: choice == .reject ? .destructive : nil) {
          Task { await submit() }
        } label: {
          if model.isSubmitting {
            ProgressView().frame(maxWidth: .infinity)
          } else {
            Label(choice.confirmTitle, systemImage: choice.symbol)
              .font(.headline)
              .frame(maxWidth: .infinity)
          }
        }
        .disabled(!canSubmit)
        .accessibilityIdentifier("confirm-decision")
      } footer: {
        Text("Signed in as \(app.userLabel). The decision is written to the audit log.")
      }
    }
    .navigationTitle(choice.title)
    .navigationBarTitleDisplayMode(.inline)
    .task { await prepare() }
    .onDisappear { model.clearDecisionError() }
  }

  private var canSubmit: Bool {
    if model.isSubmitting { return false }
    guard choice == .schedule else { return true }
    guard let scheduledFor, let now = app.serverNow else { return false }
    return ScheduleWindow.isValid(scheduledFor, serverNow: now)
  }

  private func summary(_ detail: WorkItemDetail) -> DecisionSummary {
    DecisionSummary(
      choice: choice, detail: detail, changes: model.changes, approvers: model.approvers,
      user: app.userName, scheduledFor: scheduledFor)
  }

  /// Reads the clock of the control plane for the schedule picker, then honours the
  /// development option that submits the form to show the server's refusal.
  private func prepare() async {
    if choice == .schedule {
      do {
        app.noteServerTime(try await app.controlPlane().overview().now)
        clockError = nil
      } catch {
        clockError = userMessage(error)
      }
      if scheduledFor == nil, let now = app.serverNow {
        scheduledFor = ScheduleWindow.suggested(serverNow: now)
      }
    }
    // Safety rule of the prototype: the automatic submit exists only to show how the server
    // refuses a viewer. It never runs for a user who may decide.
    if autoSubmit, choice == .requestChanges, let user = app.currentUser, !user.canDecide {
      await submit()
    }
  }

  private func submit() async {
    let accepted = await model.submit(
      choice, comment: comment, scheduledFor: scheduledFor, app: app)
    if accepted { onDone() }
  }
}

private struct ConfirmationSection: View {
  let summary: DecisionSummary

  var body: some View {
    Section {
      Text(summary.headline)
        .font(.body.weight(.semibold))
      ForEach(summary.runs, id: \.self) { run in
        Label(run, systemImage: "play.circle")
          .font(.callout.monospaced())
      }
      Text(summary.consequence)
        .font(.callout)
    } header: {
      Text("Please confirm")
    }
    .accessibilityIdentifier("decision-confirmation")
  }
}

private struct ScheduleSection: View {
  @Binding var scheduledFor: Date?
  let serverNow: Date?
  let clockError: String?

  var body: some View {
    Section {
      if let clockError {
        ErrorNotice(message: clockError)
      } else if let serverNow {
        DatePicker(
          "Start (UTC)",
          selection: Binding(
            get: { scheduledFor ?? ScheduleWindow.suggested(serverNow: serverNow) },
            set: { scheduledFor = $0 }),
          in: ScheduleWindow.earliest(serverNow: serverNow)...
        )
        .environment(\.timeZone, .gmt)
        .environment(\.calendar, Timestamps.utcCalendar)
        if let scheduledFor {
          FieldRow(
            label: "Sent to the control plane as", value: Timestamps.format(scheduledFor),
            monospaced: true)
        }
      } else {
        ProgressView("Reading the clock of the control plane")
      }
    } header: {
      Text("Start of the apply")
    } footer: {
      if let serverNow {
        Text(
          "Control-plane time now: \(Timestamps.display(serverNow)). The prototype runs on a "
            + "simulated clock, so this differs from the time of your phone.")
      }
    }
  }
}
