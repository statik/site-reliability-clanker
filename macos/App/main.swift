import AppKit
import WebKit

/// Starts the program `clanker setup` from the bundle and shows its wizard in a window.
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let backend = Backend()
    private var windows: [WebWindow] = []
    private var main: WebWindow?
    private var quitting = false
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
            if quitting { return }
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

    func applicationShouldTerminate(_: NSApplication) -> NSApplication.TerminateReply {
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
