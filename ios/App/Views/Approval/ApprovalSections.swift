import SwiftUI

struct ChangesSection: View {
  let changes: ChangeSummary

  var body: some View {
    Section {
      if changes.layers.isEmpty {
        Text("This work item has no plan. Nothing is known about what it changes.")
          .font(.callout)
          .foregroundStyle(Palette.textSecondary)
      } else {
        Text(changes.hasPlans ? changes.totalText : "Every layer is planned at apply time")
          .font(.headline)
        DestructiveCallout(resources: changes.destructive)
        ForEach(changes.layers) { layer in
          LayerChangesView(layer: layer)
        }
        SourceNote(origin: .plan)
      }
    } header: {
      Text("4 · What changes")
    }
  }
}

private struct DestructiveCallout: View {
  let resources: [ResourceLine]

  var body: some View {
    if resources.isEmpty {
      Label("Nothing is destroyed or replaced", systemImage: "checkmark.shield")
        .font(.callout)
        .foregroundStyle(Palette.textSecondary)
    } else {
      VStack(alignment: .leading, spacing: 8) {
        Label(
          "\(resources.count) destroyed or replaced", systemImage: "exclamationmark.triangle.fill"
        )
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(Palette.error)
        ForEach(resources) { resource in
          ResourceLineView(resource: resource, showsLayer: true)
        }
      }
      .accessibilityElement(children: .contain)
      .accessibilityIdentifier("destructive-callout")
    }
  }
}

private struct LayerChangesView: View {
  let layer: LayerChanges
  @State private var isExpanded = true

  var body: some View {
    DisclosureGroup(isExpanded: $isExpanded) {
      if layer.isDeferred {
        Label {
          Text(
            "Planned at apply time. That plan may only create resources. If it changes, "
              + "replaces or destroys anything, the apply stops and comes back to you.")
        } icon: {
          Image(systemName: "clock.badge.questionmark")
        }
        .font(.callout)
      }
      if layer.plannedAtApply {
        Label(
          "Planned at apply time, after the approval. The plan only created resources, so "
            + "the approval covered it.", systemImage: "clock.badge.checkmark"
        )
        .font(.caption)
        .foregroundStyle(Palette.textSecondary)
      }
      ForEach(layer.resources) { resource in
        ResourceLineView(resource: resource, showsLayer: false)
      }
    } label: {
      VStack(alignment: .leading, spacing: 2) {
        Text(Humanise.layer(layer.layer))
          .font(.subheadline.weight(.semibold))
        Text(layer.countsText)
          .font(.caption)
          .foregroundStyle(Palette.textSecondary)
      }
      .accessibilityElement(children: .combine)
    }
  }
}

struct ResourceLineView: View {
  let resource: ResourceLine
  let showsLayer: Bool

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      StatusLabel(
        text: "\(resource.action.label): \(resource.title)", symbol: resource.action.symbol,
        tone: resource.action.tone)
      if showsLayer {
        Text("in \(Humanise.layer(resource.layer))")
          .font(.caption)
          .foregroundStyle(Palette.textSecondary)
      }
      ForEach(resource.details, id: \.self) { detail in
        Text(detail)
          .font(.caption.monospaced())
      }
      Text(resource.address)
        .font(.caption2.monospaced())
        .foregroundStyle(Palette.textSecondary)
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .accessibilityElement(children: .combine)
  }
}

struct SafetySection: View {
  let explanation: Explanation

  var body: some View {
    Section {
      VStack(alignment: .leading, spacing: 4) {
        Text("Checks before the change")
          .font(.caption)
          .foregroundStyle(Palette.textSecondary)
        if explanation.preChecks.isEmpty {
          Text("Not stated").font(.callout).foregroundStyle(Palette.textSecondary)
        }
        ForEach(explanation.preChecks, id: \.self) { check in
          Label(check, systemImage: "checklist")
            .font(.callout)
        }
      }
      FieldRow(label: "Backup", value: explanation.backup)
      FieldRow(label: "Rollback", value: explanation.rollback)
      FieldRow(label: "Verification after the change", value: explanation.verification)
      SourceNote(origin: .model(source: explanation.source))
    } header: {
      Text("5 · Safety")
    }
  }
}

struct PermissionSection: View {
  let permission: PermissionSummary
  let accessError: String?

  var body: some View {
    Section {
      if permission.lines.isEmpty {
        Text("No layer of this work item needs a write permission.")
          .font(.callout)
          .foregroundStyle(Palette.textSecondary)
      }
      ForEach(permission.lines) { line in
        VStack(alignment: .leading, spacing: 4) {
          Label(line.identity, systemImage: "key.fill")
            .font(.subheadline.weight(.semibold))
          Text(line.sentence)
            .font(.callout)
          if !line.brokerRule.isEmpty {
            Text("Broker rule: \(line.brokerRule)")
              .font(.caption)
              .foregroundStyle(Palette.textSecondary)
          }
        }
        .accessibilityElement(children: .combine)
      }
      if let accessError {
        ErrorNotice(message: accessError)
      }
    } header: {
      Text("6 · Permission that your approval grants")
    } footer: {
      Text(
        "The agent has no standing write access. The broker issues the credential only "
          + "after the required approvals, and only for the commit and plan shown here.")
    }
  }
}

struct EvidenceSection: View {
  let detail: WorkItemDetail

  var body: some View {
    Section {
      FieldRow(label: "Trigger", value: detail.workItem.trigger)
      if let pull = detail.pull {
        FieldRow(
          label: "Pull request",
          value: "#\(pull.id) “\(pull.title)” by \(pull.author), branch \(pull.branch), "
            + "\(pull.state)")
      }
      FieldRow(label: "Commit", value: detail.workItem.commit, monospaced: true)
      ForEach(detail.currentPlans) { plan in
        FieldRow(label: "Plan hash, \(plan.layer)", value: plan.sha256, monospaced: true)
      }
      if !detail.diff.isEmpty {
        DisclosureGroup("Pull request diff") {
          DiffView(diff: detail.diff)
        }
      }
      NavigationLink(value: Route.activity(detail.workItem.id)) {
        Label("Timeline, jobs and logs", systemImage: "clock.arrow.circlepath")
      }
    } header: {
      Text("7 · Evidence")
    } footer: {
      Text("Your approval is bound to this commit and these plan hashes. A different plan "
        + "needs a new approval.")
    }
  }
}

struct DiffView: View {
  let diff: String

  var body: some View {
    ScrollView(.horizontal) {
      VStack(alignment: .leading, spacing: 0) {
        ForEach(Array(diff.split(separator: "\n", omittingEmptySubsequences: false).enumerated()),
          id: \.offset
        ) { _, line in
          Text(line.isEmpty ? " " : String(line))
            .font(.caption.monospaced())
            .foregroundStyle(color(of: line))
        }
      }
      .textSelection(.enabled)
    }
    .accessibilityLabel("Pull request diff")
  }

  private func color(of line: Substring) -> PaletteColor {
    if line.hasPrefix("+") && !line.hasPrefix("+++") { return Palette.success }
    if line.hasPrefix("-") && !line.hasPrefix("---") { return Palette.error }
    return Palette.text
  }
}

struct ApproversSection: View {
  let progress: ApproverProgress
  let users: [User]

  var body: some View {
    Section {
      StatusLabel(
        text: progress.headline,
        symbol: progress.isComplete ? "person.2.badge.gearshape.fill" : "person.2",
        tone: progress.isComplete ? .good : .attention)
      if progress.required > 0 {
        ProgressView(value: Double(min(progress.count, progress.required)),
          total: Double(progress.required))
        .accessibilityHidden(true)
      }
      ForEach(progress.valid) { approval in
        ApprovalLine(
          approval: approval, name: display(approval.approver), symbol: "checkmark.circle.fill",
          tone: .good, note: "Approved this commit and plan")
      }
      ForEach(progress.stale) { approval in
        ApprovalLine(
          approval: approval, name: display(approval.approver),
          symbol: "clock.arrow.trianglehead.counterclockwise.rotate.90", tone: .neutral,
          note: "Approved an earlier plan. It no longer covers this plan")
      }
      ForEach(progress.other) { approval in
        ApprovalLine(
          approval: approval, name: display(approval.approver), symbol: "arrow.uturn.backward",
          tone: .attention, note: approval.decision.replacing("_", with: " ").capitalized)
      }
    } header: {
      Text("Approvers")
    }
  }

  private func display(_ name: String) -> String {
    users.first { $0.name == name }?.display ?? name
  }
}

private struct ApprovalLine: View {
  let approval: Approval
  let name: String
  let symbol: String
  let tone: Tone
  let note: String

  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      StatusLabel(text: name, symbol: symbol, tone: tone)
      Text("\(note) · \(Timestamps.display(approval.at))")
        .font(.caption)
        .foregroundStyle(Palette.textSecondary)
      if !approval.comment.isEmpty {
        Text("“\(approval.comment)”")
          .font(.callout)
      }
    }
    .accessibilityElement(children: .combine)
  }
}
