import AppKit
import WebKit

/// Starts the program `clanker setup` from the bundle and shows its wizard in a window.
final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuItemValidation {
    private let backend = Backend()
    private var windows: [WebWindow] = []
    private var main: WebWindow?
    private var quitting = false
    private var resetting = false
    private var signals: [DispatchSourceSignal] = []

    func applicationDidFinishLaunching(_: Notification) {
        NSApp.mainMenu = Menus.build()
        let window = open(configuration: WKWebViewConfiguration())
        main = window
        window.showMessage(title: "S.R.C. starts", body: "The setup wizard opens in a moment.")
        backend.onEvent = { [weak self] event in self?.handle(event) }
        do {
            try backend.start()
        } catch {
            window.showMessage(title: "S.R.C. did not start", body: error.localizedDescription)
        }
        NSApp.activate(ignoringOtherApps: true)
        quitOnSignals()
    }

    /// A signal that `kill` sends quits the app in the usual way, so the program stops
    /// too. Without this, the program stays and holds the port of the wizard.
    private func quitOnSignals() {
        for number in [SIGTERM, SIGINT, SIGHUP] {
            signal(number, SIG_IGN)
            let source = DispatchSource.makeSignalSource(signal: number, queue: .main)
            // The quit waits for the program inside a nested run loop. A call from this
            // block would hold the main queue for that time, and the answer of the
            // program arrives on the main queue. Thus the run loop makes the call.
            source.setEventHandler {
                NSApp.perform(#selector(NSApplication.terminate(_:)), with: nil, afterDelay: 0)
            }
            source.resume()
            signals.append(source)
        }
    }

    private func handle(_ event: Backend.Event) {
        switch event {
        case let .ready(url):
            mainWindow().load(url, retries: 20)
        case let .exited(status, output):
            if quitting || resetting { return }
            mainWindow().showMessage(
                title: "S.R.C. stopped",
                body: "The program ended with status \(status). Its last lines are below. The "
                    + "full log is in \(backend.logURL.path). Quit the app and open it again.",
                detail: output
            )
        }
    }

    @discardableResult
    private func open(configuration: WKWebViewConfiguration) -> WebWindow {
        let window = WebWindow(configuration: configuration, cascadeFrom: windows.last?.window)
        window.onNewWindow = { [weak self] configuration in
            self?.open(configuration: configuration).webView ?? WKWebView()
        }
        window.onClose = { [weak self] closed in
            guard let self, closed !== self.main else { return }
            self.windows.removeAll { $0 === closed }
        }
        windows.append(window)
        window.show()
        return window
    }

    private func mainWindow() -> WebWindow {
        if let main {
            main.show()
            return main
        }
        let window = open(configuration: WKWebViewConfiguration())
        main = window
        return window
    }

    /// The control plane runs in the program, so a closed window does not quit the app.
    /// A click on the Dock icon shows the window again.
    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool {
        false
    }

    func applicationShouldHandleReopen(_: NSApplication, hasVisibleWindows visible: Bool) -> Bool {
        if !visible {
            main?.show()
        }
        return true
    }

    /// The menu item Settings. It shows the page Settings of the dashboard in the window
    /// that shows the dashboard, or in a new window when no window shows it.
    @objc func showSettings(_: Any?) {
        let port = Backend.dashboardURL.port
        let window = windows.first { $0.webView.url?.port == port }
            ?? open(configuration: WKWebViewConfiguration())
        window.show()
        window.load(Backend.dashboardURL.appendingPathComponent("settings"))
    }

    /// The menu item Reset Demo Environment. After a confirmation it stops the program,
    /// removes the local environment with its history and its saved setup, and starts
    /// the program again, which then shows the wizard from its first step.
    @objc func resetDemoEnvironment(_: Any?) {
        guard !resetting, !quitting, confirmReset() else { return }
        resetting = true
        mainWindow().showMessage(
            title: "S.R.C. resets the demo environment",
            body: "The app removes the local cluster, the work items, the audit log and the "
                + "saved setup. This can take some minutes. The setup wizard opens after that."
        )
        backend.stop { [weak self] in
            self?.backend.reset { status, output in self?.afterReset(status, output) }
        }
    }

    private func confirmReset() -> Bool {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Reset the demo environment?"
        alert.informativeText = "This removes the local environment, all work items, the "
            + "audit log and the saved setup with the key of the model. You cannot undo it."
        alert.addButton(withTitle: "Reset").hasDestructiveAction = true
        alert.addButton(withTitle: "Cancel")
        return alert.runModal() == .alertFirstButtonReturn
    }

    private func afterReset(_ status: Int32, _ output: String) {
        resetting = false
        guard status == 0 else {
            mainWindow().showMessage(
                title: "The reset did not finish",
                body: "The last lines of the reset are below. The full log is in "
                    + "\(backend.logURL.path). Quit the app and open it again, then try the "
                    + "reset again.",
                detail: output
            )
            return
        }
        mainWindow().showMessage(
            title: "S.R.C. starts", body: "The setup wizard opens in a moment."
        )
        do {
            try backend.start()
        } catch {
            mainWindow().showMessage(
                title: "S.R.C. did not start", body: error.localizedDescription
            )
        }
    }

    func validateMenuItem(_ item: NSMenuItem) -> Bool {
        // During a reset no control plane runs, so there is no page Settings to show.
        let needsBackend = [#selector(resetDemoEnvironment(_:)), #selector(showSettings(_:))]
        guard let action = item.action, resetting else { return true }
        return !needsBackend.contains(action)
    }

    func applicationShouldTerminate(_: NSApplication) -> NSApplication.TerminateReply {
        // A quit in the middle of a reset would leave the environment half removed.
        if resetting { return .terminateCancel }
        guard backend.isRunning else { return .terminateNow }
        quitting = true
        backend.stop { NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }
}

let application = NSApplication.shared
let delegate = AppDelegate()
application.delegate = delegate
application.setActivationPolicy(.regular)
application.run()
