import SwiftUI

@main
struct SRCApp: App {
  @State private var app = AppModel(options: LaunchOptions.read())

  init() {
    Palette.applyToBars()
  }

  var body: some Scene {
    WindowGroup {
      RootView()
        .paletteRoot()
        .environment(app)
        .task { await app.start() }
    }
  }
}

struct RootView: View {
  @Environment(AppModel.self) private var app

  var body: some View {
    @Bindable var app = app
    Group {
      if case .required(let notice) = app.signIn {
        SignInView(notice: notice)
      } else {
        TabView(selection: $app.selectedTab) {
          tab(.home, "Home", "house") { HomeView() }
          tab(.approvals, "Approvals", "hand.raised") { ApprovalsView() }
          tab(.activity, "Activity", "list.bullet.rectangle") { ActivityView() }
          tab(.ask, "Ask", "bubble.left.and.text.bubble.right") { AskView() }
          tab(.access, "Access", "key") { AccessView() }
        }
      }
    }
    .sheet(isPresented: $app.showSettings) {
      SettingsView()
        .paletteRoot()
    }
  }

  private func tab<Content: View>(
    _ tab: AppTab, _ title: String, _ symbol: String, @ViewBuilder content: () -> Content
  ) -> some View {
    @Bindable var app = app
    let path = Binding(
      get: { app.paths[tab] ?? [] },
      set: { app.paths[tab] = $0 })
    return NavigationStack(path: path) {
      content()
        .navigationDestination(for: Route.self) { route in
          destination(route)
        }
    }
    .tabItem { Label(title, systemImage: symbol) }
    .tag(tab)
  }

  @ViewBuilder
  private func destination(_ route: Route) -> some View {
    switch route {
    case .approval(let id): ApprovalDetailView(id: id)
    case .activity(let id): ActivityDetailView(id: id)
    case .jobLog(let id): JobLogView(jobID: id)
    case .chat(let workItem): ChatView(workItem: workItem)
    }
  }
}

/// The toolbar button that shows who is signed in and opens Settings.
struct AccountButton: View {
  @Environment(AppModel.self) private var app

  var body: some View {
    Button {
      app.showSettings = true
    } label: {
      Label(app.userLabel, systemImage: "person.crop.circle")
        .labelStyle(.titleAndIcon)
        .font(.subheadline)
    }
    .accessibilityLabel("Signed in as \(app.userLabel). Open settings")
    .accessibilityIdentifier("account-button")
  }
}
