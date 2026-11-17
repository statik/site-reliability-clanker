import SwiftUI

/// The screen of a control plane that asks for a sign-in. It runs the device flow: the app
/// shows a code, and a person who is signed in to the dashboard approves it in a browser.
struct SignInView: View {
  let notice: String?
  @Environment(AppModel.self) private var app
  @Environment(\.openURL) private var openURL
  @State private var model = SignInModel()

  var body: some View {
    NavigationStack {
      Form {
        Section {
          HStack(spacing: 12) {
            PaperBoatMark(size: 36)
            Text("The control plane at \(app.settings.controlPlane) asks you to sign in.")
              .font(.callout)
          }
          if let notice {
            Label(notice, systemImage: "info.circle")
              .font(.callout)
              .accessibilityIdentifier("sign-in-notice")
          }
        }
        switch model.step {
        case .idle:
          Section {
            Button("Sign in") { model.begin(app: app) }
              .accessibilityIdentifier("sign-in-button")
          } footer: {
            Text(
              "The app shows a code. You approve the code in a browser where you are "
                + "signed in to the dashboard.")
          }
        case .requestingCode:
          Section { ProgressView("Asking for a code") }
        case .waiting(let grant):
          CodeSection(grant: grant) { openURL(grant.pageWithCode) }
          Section {
            ProgressView("Waiting for the approval")
            Button("Cancel", role: .cancel) { model.cancel() }
          }
        case .failed(let message):
          Section {
            ErrorNotice(message: message, status: "The sign-in did not finish") {
              model.begin(app: app)
            }
          }
        }
      }
      .navigationTitle("Sign in")
      .toolbar {
        ToolbarItem(placement: .topBarTrailing) {
          Button("Settings", systemImage: "gearshape") { app.showSettings = true }
            .accessibilityIdentifier("sign-in-settings")
        }
      }
      .onChange(of: app.settings.controlPlane) { model.cancel() }
      .task {
        if app.options.signIn { model.begin(app: app) }
      }
    }
  }
}

private struct CodeSection: View {
  let grant: DeviceGrant
  let open: () -> Void

  var body: some View {
    Section {
      Text(grant.userCode)
        .font(.system(.largeTitle, design: .monospaced).weight(.semibold))
        .frame(maxWidth: .infinity)
        .textSelection(.enabled)
        .accessibilityLabel("Code \(grant.userCode.map(String.init).joined(separator: " "))")
        .accessibilityIdentifier("sign-in-code")
      FieldRow(label: "Page", value: grant.page.absoluteString)
      Button("Open the page in the browser", systemImage: "safari", action: open)
        .accessibilityIdentifier("sign-in-open-page")
    } header: {
      Text("Your code")
    } footer: {
      Text(
        "Open the page in a browser where you are signed in to the dashboard. Approve only "
          + "when the page shows this code. The code is valid for "
          + "\(max(grant.expiresIn / 60, 1)) minutes.")
    }
  }
}
