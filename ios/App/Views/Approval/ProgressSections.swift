import SwiftUI

/// The apply as it happens: each layer, its short-lived permission, the verification and
/// the run log. New rows arrive over the event stream.
struct ProgressSections: View {
  let progress: ApplyProgress
  let state: WorkState

  private var title: String {
    if state.isRunning { return "Progress (live)" }
    if state == .awaitingApproval { return "Earlier attempt" }
    return "Latest apply"
  }

  var body: some View {
    Section {
      if !progress.refusal.isEmpty {
        Label {
          Text("The runner refused the apply: \(progress.refusal)")
        } icon: {
          Image(systemName: "hand.raised.slash.fill").foregroundStyle(.red)
        }
        .font(.callout)
      }
      ForEach(progress.layers) { layer in
        LayerProgressRow(layer: layer)
      }
      VerificationRow(result: progress.verification, state: state)
    } header: {
      Text(title)
    } footer: {
      Text("Read from the audit log. The permission of a layer exists only while its job runs.")
    }
    Section("Run log") {
      ForEach(progress.steps) { step in
        StepRow(step: step)
      }
    }
  }
}

private struct LayerProgressRow: View {
  let layer: LayerProgress

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      AdaptiveRow {
        Text(Humanise.layer(layer.layer))
          .font(.subheadline.weight(.semibold))
      } trailing: {
        HStack {
          if layer.status == .applying {
            ProgressView().controlSize(.small)
          }
          StatusLabel(
            text: layer.status.label, symbol: layer.status.symbol, tone: layer.status.tone)
        }
      }
      Label(layer.grant.text, systemImage: layer.grant.symbol)
        .font(.caption)
        .foregroundStyle(.secondary)
      if !layer.note.isEmpty {
        Text(layer.note)
          .font(.caption)
          .foregroundStyle(layer.status == .failed ? Color.red : Color.secondary)
      }
      if let jobID = layer.jobID {
        NavigationLink(value: Route.jobLog(jobID)) {
          Text("Log of job \(jobID)").font(.caption)
        }
      }
    }
    .accessibilityElement(children: .contain)
  }
}

private struct VerificationRow: View {
  let result: VerificationResult
  let state: WorkState

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      switch result {
      case .pending:
        StatusLabel(
          text: state == .verifying ? "Verification runs" : "Verification: no result",
          symbol: "stethoscope", tone: .neutral)
      case .passed(let checks):
        StatusLabel(text: "Verification passed", symbol: "checkmark.seal.fill", tone: .good)
        checkList(checks)
      case .failed(let reason, let checks):
        StatusLabel(text: "Verification failed", symbol: "xmark.seal.fill", tone: .bad)
        Text(reason).font(.callout)
        checkList(checks)
      }
    }
    .accessibilityElement(children: .combine)
  }

  private func checkList(_ checks: [String]) -> some View {
    ForEach(checks, id: \.self) { check in
      Text("• \(check)")
        .font(.caption)
        .foregroundStyle(.secondary)
    }
  }
}

struct StepRow: View {
  let step: ProgressStep

  var body: some View {
    HStack(alignment: .top, spacing: 10) {
      Image(systemName: step.symbol)
        .foregroundStyle(step.tone.color)
        .frame(width: 22)
        .accessibilityHidden(true)
      VStack(alignment: .leading, spacing: 2) {
        Text(step.title)
          .font(.callout)
        if !step.detail.isEmpty {
          Text(step.detail)
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        Text(Timestamps.display(step.time))
          .font(.caption2.monospacedDigit())
          .foregroundStyle(.tertiary)
      }
    }
    .accessibilityElement(children: .combine)
  }
}
