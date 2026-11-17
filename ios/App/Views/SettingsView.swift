import SwiftUI

struct SettingsView: View {
  @Environment(AppModel.self) private var app
  @Environment(\.dismiss) private var dismiss
  @State private var controlPlane = ""
  @State private var paperboat = ""
  @State private var addressError: String?

  var body: some View {
    NavigationStack {
      PaletteForm {
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
          addressField("S.R.C. control plane", text: $controlPlane)
          addressField("Paperboat", text: $paperboat)
          if let addressError {
            ErrorNotice(message: addressError)
          }
          Button("Use these addresses") { saveAddresses() }
            .foregroundStyle(Palette.interactive)
            .disabled(!hasAddressChanges)
          Button("Reset to defaults") {
            controlPlane = AppSettings.defaultControlPlane
            paperboat = AppSettings.defaultPaperboat
            saveAddresses()
          }
          .foregroundStyle(Palette.interactive)
        } header: {
          Text("Servers")
        } footer: {
          Text(
            "Plain HTTP works only for addresses on your local network. The iOS Simulator "
              + "reaches the servers of your Mac through localhost.")
        }
        Section("About") {
          HStack(spacing: 12) {
            ProductMark(size: 44)
            Text(
              "This app shows how Paperboat, the fish rental business, runs and lets you "
                + "decide on the work that S.R.C. (Site Reliability Clanker) proposes.")
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
        .foregroundStyle(Palette.textSecondary)
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
    if case .required = app.signIn {
      Section("Signed in as") {
        Text("Nobody. The control plane asks for a sign-in: close Settings to sign in.")
          .font(.callout)
        keychainProblem
      }
    } else if app.usesToken {
      person
    } else {
      DemoUserSection()
    }
  }

  private var person: some View {
    Section {
      if let me = app.me {
        FieldRow(label: "Name", value: me.display)
        FieldRow(label: "Email", value: me.email)
        FieldRow(label: "Role", value: me.role)
      } else {
        Text("The control plane did not say yet who you are.").font(.callout)
      }
      keychainProblem
      Button("Sign out", role: .destructive) { app.signOut() }
        .foregroundStyle(Palette.error)
        .accessibilityIdentifier("sign-out")
    } header: {
      Text("Signed in as")
    } footer: {
      Text(
        "Sign out removes the token from this phone. The control plane has no way to end "
          + "one token early: the token stays valid until twelve hours after the sign-in.")
    }
  }

  @ViewBuilder private var keychainProblem: some View {
    if let problem = app.keychainProblem {
      ErrorNotice(message: problem)
    }
  }
}

private struct DemoUserSection: View {
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
        "This control plane has no sign-in: the app sends the name you pick here in the "
          + "X-Clanker-User header, and anyone who can reach the server can do the same.")
    }
  }

  private var selection: Binding<String> {
    Binding(
      get: { app.settings.user },
      set: { app.selectUser($0) })
  }
}
