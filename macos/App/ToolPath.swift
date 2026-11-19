import Foundation

/// The search path for the tools that `clanker` starts: docker, kind, tofu, ollama, aws.
///
/// An app that Finder starts gets only the system directories. A terminal gets more from
/// the files of the shell. This type asks the login shell for its path and adds the usual
/// install directories, so the app finds the same tools as a terminal.
enum ToolPath {
    private static let begin = "__SRC_PATH_BEGIN__"
    private static let end = "__SRC_PATH_END__"

    static func value() -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let usual = [
            "/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin",
            "\(home)/.docker/bin", "/Applications/Docker.app/Contents/Resources/bin",
            "\(home)/.local/bin", "\(home)/bin", "\(home)/go/bin",
            "/usr/bin", "/bin", "/usr/sbin", "/sbin",
        ]
        let inherited = (ProcessInfo.processInfo.environment["PATH"] ?? "")
            .split(separator: ":").map(String.init)
        var seen = Set<String>()
        let all = (fromLoginShell() + usual + inherited).filter { part in
            !part.isEmpty && seen.insert(part).inserted
        }
        return all.joined(separator: ":")
    }

    /// Runs the login shell and reads its PATH. Returns nothing when the shell is slow or
    /// fails: the usual directories are then enough for a standard install.
    private static func fromLoginShell() -> [String] {
        let shell = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
        let process = Process()
        process.executableURL = URL(fileURLWithPath: shell)
        process.arguments = ["-l", "-i", "-c", "printf '%s%s%s' \(begin) \"$PATH\" \(end)"]
        process.standardInput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        let output = Pipe()
        process.standardOutput = output
        do {
            try process.run()
        } catch {
            return []
        }
        let finished = DispatchSemaphore(value: 0)
        var data = Data()
        DispatchQueue.global().async {
            data = output.fileHandleForReading.readDataToEndOfFile()
            finished.signal()
        }
        if finished.wait(timeout: .now() + 4) == .timedOut {
            process.terminate()
            return []
        }
        let text = String(decoding: data, as: UTF8.self)
        guard let from = text.range(of: begin), let to = text.range(of: end),
              from.upperBound <= to.lowerBound
        else {
            return []
        }
        return text[from.upperBound..<to.lowerBound].split(separator: ":").map(String.init)
    }
}
