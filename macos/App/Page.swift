import Foundation

/// Small helpers for the pages that the app shows by itself.
enum Page {
    /// An address on this machine stays in the app. Other addresses open in the browser.
    static func isLocal(_ url: URL) -> Bool {
        guard let scheme = url.scheme else { return false }
        if ["about", "blob", "data"].contains(scheme) { return true }
        guard let host = url.host else { return false }
        return host == "localhost" || host == "::1" || host.hasPrefix("127.")
    }

    /// A file name in Downloads that no file has yet.
    static func freeName(_ suggested: String) -> URL {
        let files = FileManager.default
        let folder = files.urls(for: .downloadsDirectory, in: .userDomainMask).first
            ?? files.homeDirectoryForCurrentUser
        let name = suggested.isEmpty ? "download" : (suggested as NSString).lastPathComponent
        let base = (name as NSString).deletingPathExtension
        let ext = (name as NSString).pathExtension
        var candidate = folder.appendingPathComponent(name)
        var number = 2
        while files.fileExists(atPath: candidate.path) {
            let next = ext.isEmpty ? "\(base) \(number)" : "\(base) \(number).\(ext)"
            candidate = folder.appendingPathComponent(next)
            number += 1
        }
        return candidate
    }

    static func message(title: String, body: String, detail: String) -> String {
        let block = detail.isEmpty ? "" : "<pre>\(escape(detail))</pre>"
        return """
        <!doctype html><html><head><meta charset="utf-8"><style>
        :root { color-scheme: light dark; }
        body { font: 15px/1.5 -apple-system, sans-serif; margin: 0; padding: 12vh 8vw;
               background: #F7ECD7; color: #1B354A; }
        h1 { font-size: 26px; margin: 0 0 12px; }
        p { max-width: 60ch; margin: 0 0 16px; }
        pre { background: #1B354A; color: #F7ECD7; padding: 16px; border-radius: 8px;
              white-space: pre-wrap; word-break: break-word; font: 12px/1.5 ui-monospace, Menlo;
              max-height: 45vh; overflow: auto; }
        @media (prefers-color-scheme: dark) {
          body { background: #1C1917; color: #F7ECD7; }
          pre { background: #292524; }
        }
        </style></head><body><h1>\(escape(title))</h1><p>\(escape(body))</p>\(block)</body></html>
        """
    }

    private static func escape(_ text: String) -> String {
        text.replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
    }
}
