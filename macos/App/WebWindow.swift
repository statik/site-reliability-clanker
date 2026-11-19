import AppKit
import os
import WebKit

/// One window with a web view. It shows the wizard, the dashboard, and the pages that
/// they open in a new window.
final class WebWindow: NSObject, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate,
    WKDownloadDelegate
{
    let window: NSWindow
    let webView: WKWebView
    /// Called when a page asks for a new window for an address on this machine.
    var onNewWindow: ((WKWebViewConfiguration) -> WKWebView)?
    var onClose: ((WebWindow) -> Void)?

    private static let log = Logger(subsystem: "io.github.statik.clanker", category: "window")
    private var retriesLeft = 0
    private var retryURL: URL?

    init(configuration: WKWebViewConfiguration = WKWebViewConfiguration(), cascadeFrom: NSWindow?) {
        webView = WKWebView(frame: .zero, configuration: configuration)
        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1240, height: 860),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false
        )
        super.init()
        window.title = "S.R.C."
        window.minSize = NSSize(width: 720, height: 520)
        window.isReleasedWhenClosed = false
        window.contentView = webView
        window.delegate = self
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        if let other = cascadeFrom {
            window.setFrameTopLeftPoint(window.cascadeTopLeft(from: other.frame.origin))
        } else {
            window.center()
            window.setFrameAutosaveName("main")
        }
    }

    func show() {
        window.makeKeyAndOrderFront(nil)
    }

    /// Loads an address. A server that does not listen yet gets `retries` more tries.
    func load(_ url: URL, retries: Int = 0) {
        retriesLeft = retries
        retryURL = url
        webView.load(URLRequest(url: url))
    }

    func showMessage(title: String, body: String, detail: String = "") {
        retryURL = nil
        webView.loadHTMLString(Page.message(title: title, body: body, detail: detail), baseURL: nil)
    }

    func windowWillClose(_: Notification) {
        onClose?(self)
    }

    // MARK: navigation

    func webView(
        _: WKWebView, decidePolicyFor action: WKNavigationAction,
        decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
    ) {
        if action.shouldPerformDownload {
            decisionHandler(.download)
            return
        }
        guard let url = action.request.url, action.targetFrame?.isMainFrame ?? true else {
            decisionHandler(.allow)
            return
        }
        if Page.isLocal(url) {
            decisionHandler(.allow)
        } else {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
        }
    }

    func webView(
        _: WKWebView, decidePolicyFor response: WKNavigationResponse,
        decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void
    ) {
        decisionHandler(response.canShowMIMEType ? .allow : .download)
    }

    func webView(_: WKWebView, didFinish _: WKNavigation!) {
        if let title = webView.title, !title.isEmpty {
            window.title = title
        }
        let host = webView.url?.host ?? "a page of the app"
        let port = webView.url?.port ?? 0
        let title = window.title
        WebWindow.log.info("loaded \(host, privacy: .public):\(port) \(title, privacy: .public)")
    }

    func webView(
        _: WKWebView, didFailProvisionalNavigation _: WKNavigation!, withError error: Error
    ) {
        guard let url = retryURL else { return }
        if retriesLeft > 0 {
            retriesLeft -= 1
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [weak self] in
                self?.webView.load(URLRequest(url: url))
            }
            return
        }
        showMessage(
            title: "The page did not load",
            body: "S.R.C. did not answer at \(url.host ?? "this machine"). Quit the app and "
                + "open it again.",
            detail: error.localizedDescription
        )
    }

    // MARK: new windows and dialogs

    func webView(
        _: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
        for action: WKNavigationAction, windowFeatures _: WKWindowFeatures
    ) -> WKWebView? {
        guard let url = action.request.url else { return nil }
        if Page.isLocal(url) {
            return onNewWindow?(configuration)
        }
        NSWorkspace.shared.open(url)
        return nil
    }

    func webView(
        _: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
        initiatedByFrame _: WKFrameInfo, completionHandler: @escaping () -> Void
    ) {
        let alert = NSAlert()
        alert.messageText = message
        alert.beginSheetModal(for: window) { _ in completionHandler() }
    }

    func webView(
        _: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
        initiatedByFrame _: WKFrameInfo, completionHandler: @escaping (Bool) -> Void
    ) {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        alert.beginSheetModal(for: window) { completionHandler($0 == .alertFirstButtonReturn) }
    }

    // MARK: downloads

    func webView(
        _: WKWebView, navigationAction _: WKNavigationAction, didBecome download: WKDownload
    ) {
        download.delegate = self
    }

    func webView(
        _: WKWebView, navigationResponse _: WKNavigationResponse, didBecome download: WKDownload
    ) {
        download.delegate = self
    }

    private var saved: [ObjectIdentifier: URL] = [:]

    func download(
        _ download: WKDownload, decideDestinationUsing _: URLResponse, suggestedFilename: String,
        completionHandler: @escaping (URL?) -> Void
    ) {
        let target = Page.freeName(suggestedFilename)
        saved[ObjectIdentifier(download)] = target
        completionHandler(target)
    }

    func downloadDidFinish(_ download: WKDownload) {
        if let file = saved.removeValue(forKey: ObjectIdentifier(download)) {
            NSWorkspace.shared.activateFileViewerSelecting([file])
        }
    }

    func download(_ download: WKDownload, didFailWithError error: Error, resumeData _: Data?) {
        saved.removeValue(forKey: ObjectIdentifier(download))
        let alert = NSAlert()
        alert.messageText = "The download did not finish"
        alert.informativeText = error.localizedDescription
        alert.beginSheetModal(for: window)
    }
}
