import Foundation

/// Runs the `clanker setup` program that is inside the app bundle.
///
/// The program gets its own process group. A stop sends the interrupt signal to the group,
/// as Ctrl+C does in a terminal, so the program and the tools that it started stop together.
final class Backend {
    enum Event {
        /// The wizard listens. The address has the token of this run.
        case ready(URL)
        /// The program ended. The text is the last part of its output.
        case exited(status: Int32, output: String)
    }

    /// Called on the main queue.
    var onEvent: ((Event) -> Void)?

    private let lock = NSLock()
    private var pid: pid_t = 0
    private var running = false
    private var announced = false
    private var tail = ""
    private let log: FileHandle?
    let logURL: URL

    init() {
        let folder = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/Site Reliability Clanker")
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        logURL = folder.appendingPathComponent("clanker.log")
        FileManager.default.createFile(atPath: logURL.path, contents: nil)
        log = try? FileHandle(forWritingTo: logURL)
    }

    var isRunning: Bool {
        lock.lock()
        defer { lock.unlock() }
        return running
    }

    /// Starts the program. Throws when the program is not in the bundle or cannot start.
    func start() throws {
        guard let binary = Bundle.main.url(forAuxiliaryExecutable: "clanker") else {
            throw BackendError.missingBinary
        }
        endLeftovers()
        var fds: [Int32] = [0, 0]
        guard pipe(&fds) == 0 else { throw BackendError.spawn(errno) }
        let child = try spawn(binary: binary.path, output: fds[1])
        close(fds[1])
        lock.lock()
        pid = child
        running = true
        lock.unlock()
        let reader = FileHandle(fileDescriptor: fds[0], closeOnDealloc: true)
        Thread.detachNewThread { [weak self] in self?.pump(reader, child: child) }
    }

    /// Stops the program and calls `done` on the main queue when it is gone.
    func stop(then done: @escaping () -> Void) {
        lock.lock()
        let group = pid
        let alive = running
        lock.unlock()
        guard alive else {
            done()
            return
        }
        kill(-group, SIGINT)
        DispatchQueue.global().async { [weak self] in
            let deadline = Date().addingTimeInterval(20)
            while self?.isRunning == true, Date() < deadline {
                usleep(100_000)
            }
            if self?.isRunning == true {
                kill(-group, SIGKILL)
            }
            DispatchQueue.main.async(execute: done)
        }
    }

    /// Ends a program that an earlier start of the app left, for example after a forced
    /// quit. Such a program holds the ports, and a new one cannot start beside it.
    private func endLeftovers() {
        let old = Backend.pids(matching: "MacOS/clanker setup --app")
        guard !old.isEmpty else { return }
        old.forEach { Backend.signal($0, SIGINT) }
        let deadline = Date().addingTimeInterval(15)
        while Date() < deadline, old.contains(where: { kill($0, 0) == 0 }) {
            usleep(200_000)
        }
        old.filter { kill($0, 0) == 0 }.forEach { Backend.signal($0, SIGKILL) }
    }

    /// Sends a signal to the process group of a program, or to the program when it has
    /// no group of its own.
    private static func signal(_ pid: pid_t, _ number: Int32) {
        if kill(-pid, number) != 0 {
            kill(pid, number)
        }
    }

    private static func pids(matching pattern: String) -> [pid_t] {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/pgrep")
        process.arguments = ["-f", pattern]
        let output = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        guard (try? process.run()) != nil else { return [] }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return String(decoding: data, as: UTF8.self).split(separator: "\n")
            .compactMap { pid_t($0) }
    }

    private func spawn(binary: String, output: Int32) throws -> pid_t {
        var actions: posix_spawn_file_actions_t?
        posix_spawn_file_actions_init(&actions)
        defer { posix_spawn_file_actions_destroy(&actions) }
        posix_spawn_file_actions_addopen(&actions, 0, "/dev/null", O_RDONLY, 0)
        posix_spawn_file_actions_adddup2(&actions, output, 1)
        posix_spawn_file_actions_adddup2(&actions, output, 2)

        var attributes: posix_spawnattr_t?
        posix_spawnattr_init(&attributes)
        defer { posix_spawnattr_destroy(&attributes) }
        posix_spawnattr_setpgroup(&attributes, 0)
        // The app ignores the signals that it handles by itself. The program must not.
        var defaults = sigset_t()
        sigemptyset(&defaults)
        for number in [SIGINT, SIGTERM, SIGHUP] {
            sigaddset(&defaults, number)
        }
        posix_spawnattr_setsigdefault(&attributes, &defaults)
        let flags = POSIX_SPAWN_SETPGROUP | POSIX_SPAWN_CLOEXEC_DEFAULT | POSIX_SPAWN_SETSIGDEF
        posix_spawnattr_setflags(&attributes, Int16(flags))

        var environment = ProcessInfo.processInfo.environment
        environment["PATH"] = ToolPath.value()
        let arguments = [binary, "setup", "--app"]
        var argv = arguments.map { strdup($0) } + [nil]
        var envp = environment.map { strdup("\($0.key)=\($0.value)") } + [nil]
        defer {
            argv.forEach { free($0) }
            envp.forEach { free($0) }
        }
        var child: pid_t = 0
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        FileManager.default.changeCurrentDirectoryPath(home)
        let code = posix_spawn(&child, binary, &actions, &attributes, &argv, &envp)
        guard code == 0 else { throw BackendError.spawn(code) }
        return child
    }

    /// Reads the output until the program closes it, then reports the exit.
    private func pump(_ reader: FileHandle, child: pid_t) {
        while true {
            let data = reader.availableData
            if data.isEmpty { break }
            log?.write(data)
            consume(String(decoding: data, as: UTF8.self))
        }
        var status: Int32 = 0
        waitpid(child, &status, 0)
        lock.lock()
        running = false
        let output = tail
        lock.unlock()
        let code = (status & 0x7F) == 0 ? (status >> 8) & 0xFF : -(status & 0x7F)
        DispatchQueue.main.async { [weak self] in
            self?.onEvent?(.exited(status: code, output: output))
        }
    }

    private func consume(_ text: String) {
        lock.lock()
        tail = String((tail + text).suffix(4000))
        let found = announced ? nil : Backend.wizardAddress(in: tail)
        if found != nil { announced = true }
        lock.unlock()
        if let found {
            DispatchQueue.main.async { [weak self] in self?.onEvent?(.ready(found)) }
        }
    }

    /// Finds the address with the token that `clanker setup` prints.
    static func wizardAddress(in text: String) -> URL? {
        let pattern = #"http://[0-9A-Za-z\.\[\]:]+/\?token=[0-9A-Za-z_\-]+"#
        guard let range = text.range(of: pattern, options: .regularExpression) else {
            return nil
        }
        return URL(string: String(text[range]))
    }
}

enum BackendError: LocalizedError {
    case missingBinary
    case spawn(Int32)

    var errorDescription: String? {
        switch self {
        case .missingBinary:
            return "The program clanker is not inside this app. Download the app again."
        case let .spawn(code):
            return "The program clanker did not start: \(String(cString: strerror(code)))."
        }
    }
}
