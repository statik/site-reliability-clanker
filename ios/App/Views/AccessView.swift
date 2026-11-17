import SwiftUI

/// Who and what holds which permission right now, and the recent grants.
struct AccessView: View {
  @Environment(AppModel.self) private var app
  @State private var model = AccessModel()

  var body: some View {
    List {
      if let error = model.error {
        Section { ErrorNotice(message: error) { Task { await model.load(app: app) } } }
      }
      if let report = model.report {
        ActiveGrantsSection(grants: report.active)
        Section {
          ForEach(report.identities) { identity in
            IdentityRow(identity: identity)
          }
        } header: {
          Text("Identities of the agent")
        } footer: {
          Text("Standing access is what an identity holds all the time. Everything else "
            + "needs a grant.")
        }
        PeopleSection(users: app.users)
        Section("Recent grants") {
          ForEach(AccessModel.recent(report.history, limit: 20)) { grant in
            GrantRow(grant: grant)
          }
        }
      }
    }
    .navigationTitle("Access")
    .toolbar {
      ToolbarItem(placement: .topBarTrailing) { AccountButton() }
    }
    .overlay {
      if model.report == nil && model.error == nil { ProgressView() }
    }
    .refreshable { await model.load(app: app) }
    .task(id: app.generation) { await model.load(app: app) }
    .onChange(of: app.latestEvent?.seq) {
      guard model.consume(app.recentEvents) else { return }
      Task { await model.load(app: app) }
    }
  }
}

private struct ActiveGrantsSection: View {
  let grants: [Grant]

  var body: some View {
    Section {
      if grants.isEmpty {
        VStack(alignment: .leading, spacing: 6) {
          StatusLabel(text: "No write permission is active", symbol: "lock.fill", tone: .good)
          Text("Right now the agent can only read.")
            .font(.callout)
            .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
      }
      ForEach(grants) { grant in
        GrantRow(grant: grant)
      }
    } header: {
      Text("Active right now")
    }
  }
}

private struct IdentityRow: View {
  let identity: AccessIdentity

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Label(identity.name, systemImage: "person.badge.key")
        .font(.subheadline.weight(.semibold))
      FieldRow(label: "Standing access", value: identity.standing)
      FieldRow(label: "With a grant", value: identity.elevation)
    }
    .accessibilityElement(children: .combine)
  }
}

private struct PeopleSection: View {
  let users: [User]

  var body: some View {
    if !users.isEmpty {
      Section {
        ForEach(users) { user in
          LabeledContent {
            StatusLabel(
              text: user.role.capitalized,
              symbol: user.canDecide ? "checkmark.seal" : "eye", tone: .neutral)
          } label: {
            Text(user.display)
          }
        }
      } header: {
        Text("People")
      } footer: {
        Text("Approvers and admins may decide on changes. Viewers may only read.")
      }
    }
  }
}

private struct GrantRow: View {
  let grant: Grant

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      AdaptiveRow {
        Text(grant.identity)
          .font(.subheadline.weight(.semibold))
      } trailing: {
        StatusLabel(text: statusText, symbol: statusSymbol, tone: statusTone)
      }
      Text(scope)
        .font(.callout)
      Text("\(grant.id) · \(Timestamps.display(grant.issuedAt))\(lifetime)")
        .font(.caption.monospacedDigit())
        .foregroundStyle(.secondary)
      if !grant.approvers.isEmpty {
        Text("Approved by: \(grant.approvers.joined(separator: ", "))")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
    }
    .accessibilityElement(children: .combine)
  }

  private var scope: String {
    let target = grant.workItem.isEmpty ? "" : " for \(grant.workItem)"
    return "\(grant.kind.capitalized) on \(grant.layer) in \(grant.env)\(target)"
  }

  private var lifetime: String {
    Timestamps.span(from: grant.notBefore, to: grant.expires).map { " · valid \($0)" } ?? ""
  }

  private var statusText: String {
    switch grant.status {
    case "issued", "active": "Active"
    case "used": "Used, ended"
    case "revoked": "Revoked"
    case "expired": "Expired"
    default: grant.status.capitalized
    }
  }

  private var statusSymbol: String {
    switch grant.status {
    case "issued", "active": "key.fill"
    case "used": "checkmark.circle"
    case "revoked": "lock.slash"
    default: "clock"
    }
  }

  private var statusTone: Tone {
    grant.isActive ? .attention : .neutral
  }
}
