import SwiftUI

struct SettingsView: View {
  @Environment(AppModel.self) private var app
  @Environment(\.dismiss) private var dismiss
  @State private var controlPlane = ""
  @State private var paperboat = ""
  @State private var addressError: String?

  var body: some View {
    NavigationStack {
      Form {
        SignedInSection()
        Section("Connection") {
          StatusLabel(
            text: "Event stream: \(app.streamStatus.label)", symbol: app.streamStatus.symbol,
            tone: app.streamStatus == .connected ? .good : .attention)
          if case .disconnected(let reason) = app.streamStatus {
            Text(reason).font(.callout)
          }
          if let event = app.latestEvent {
            FieldRow(
              label: "Last event received",
              value: "#\(event.seq) \(event.type) · \(Timestamps.display(event.ts))")
          }
          if let problem = app.streamProblem {
            ErrorNotice(message: problem)
          }
        }
        Section {
          addressField("Clanker control plane", text: $controlPlane)
          addressField("Paperboat", text: $paperboat)
          if let addressError {
            ErrorNotice(message: addressError)
          }
          Button("Use these addresses") { saveAddresses() }
            .disabled(!hasAddressChanges)
          Button("Reset to defaults") {
            controlPlane = AppSettings.defaultControlPlane
            paperboat = AppSettings.defaultPaperboat
            saveAddresses()
          }
        } header: {
          Text("Servers")
        } footer: {
          Text(
            "Plain HTTP works only for addresses on your local network. The iOS Simulator "
              + "reaches the servers of your Mac through localhost.")
        }
        Section("About") {
          HStack(spacing: 12) {
            PaperBoatMark(size: 36)
            Text(
              "Paperboat Manager shows how the fish rental business runs and lets you decide "
                + "on the work that Site Reliability Clanker proposes.")
            .font(.callout)
          }
        }
      }
      .navigationTitle("Settings")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar {
        ToolbarItem(placement: .confirmationAction) {
          Button("Done") { dismiss() }
        }
      }
      .onAppear {
        controlPlane = app.settings.controlPlane
        paperboat = app.settings.paperboat
      }
    }
  }

  private var hasAddressChanges: Bool {
    controlPlane != app.settings.controlPlane || paperboat != app.settings.paperboat
  }

  private func addressField(_ label: String, text: Binding<String>) -> some View {
    VStack(alignment: .leading, spacing: 2) {
      Text(label)
        .font(.caption)
        .foregroundStyle(.secondary)
      TextField(label, text: text)
        .keyboardType(.URL)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .font(.callout.monospaced())
    }
  }

  private func saveAddresses() {
    do {
      _ = try AppSettings.baseURL(controlPlane)
      _ = try AppSettings.baseURL(paperboat)
    } catch {
      addressError = userMessage(error)
      return
    }
    addressError = nil
    var settings = app.settings
    settings.controlPlane = controlPlane.trimmingCharacters(in: .whitespacesAndNewlines)
    settings.paperboat = paperboat.trimmingCharacters(in: .whitespacesAndNewlines)
    app.update(settings)
  }
}

private struct SignedInSection: View {
  @Environment(AppModel.self) private var app

  var body: some View {
    Section {
      if let error = app.usersError {
        ErrorNotice(message: error) { Task { await app.loadUsers() } }
      }
      Picker("Signed in as", selection: selection) {
        if app.currentUser == nil {
          Text(app.settings.user).tag(app.settings.user)
        }
        ForEach(app.users) { user in
          Text("\(user.display) · \(user.role)").tag(user.name)
        }
      }
      .pickerStyle(.inline)
      .labelsHidden()
      .accessibilityIdentifier("user-picker")
    } header: {
      Text("Signed in as")
    } footer: {
      Text(
        "This prototype has no real sign-in: the app sends the name you pick here in the "
          + "X-Clanker-User header, and anyone who can reach the server can do the same.")
    }
  }

  private var selection: Binding<String> {
    Binding(
      get: { app.settings.user },
      set: { app.selectUser($0) })
  }
}
