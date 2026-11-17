import Foundation

/// The wacli program inside the bundle.
enum Wacli {
    static let program = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/wacli")

    struct Failure: Error { let message: String }

    private struct Envelope<T: Decodable>: Decodable {
        let data: T?
        let error: String?
    }

    /// Runs one command with `--json` and returns its `data`. A failure
    /// carries the message wacli gave.
    static func json<T: Decodable>(_ type: T.Type, _ arguments: [String], input: String? = nil) async throws -> T {
        let process = Process()
        process.executableURL = program
        process.arguments = ["--json"] + arguments
        let stdout = Pipe(), stderr = Pipe(), stdin = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr
        process.standardInput = stdin
        try process.run()
        if let input { stdin.fileHandleForWriting.write(Data(input.utf8)) }
        try stdin.fileHandleForWriting.close()

        // Each pipe is read on its own thread, so a full one cannot stall the program.
        async let output = Task.detached { stdout.fileHandleForReading.readDataToEndOfFile() }.value
        async let errors = Task.detached { stderr.fileHandleForReading.readDataToEndOfFile() }.value
        let (out, err) = await (output, errors)
        process.waitUntilExit()

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        if process.terminationStatus == 0, let data = try decoder.decode(Envelope<T>.self, from: out).data {
            return data
        }
        // A failure is the same envelope on stderr, on its last line.
        let last = String(decoding: err, as: UTF8.self).split(separator: "\n").last.map(String.init) ?? ""
        let message = (try? decoder.decode(Envelope<Bool>.self, from: Data(last.utf8)))?.error
        throw Failure(message: message ?? (last.isEmpty ? "wacli exited with status \(process.terminationStatus)" : last))
    }
}

/// Linking WhatsApp: runs `wacli auth --page` and tells the window where the
/// page is. The page itself follows the pairing: the code, that it is linked,
/// how many messages came in, that it stopped.
@MainActor
final class Linking: ObservableObject {
    /// The pairing page, once wacli has written it.
    @Published private(set) var page: URL?
    /// Why the last run ended early, in wacli's words.
    @Published private(set) var failure: String?
    @Published private(set) var running = false

    private var process: Process?

    func start() {
        guard !running else { return }
        failure = nil
        running = true

        let process = Process()
        process.executableURL = Wacli.program
        // A scheduled push can hold the store for the length of one sync.
        process.arguments = ["--lock-wait", "2m", "--events", "auth", "--page", "--lang", texts.language]
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        let events = Pipe()
        process.standardError = events

        Task.detached { [weak self] in
            var lastError: String?
            do {
                for try await line in events.fileHandleForReading.bytes.lines {
                    guard let event = try? JSONDecoder().decode(Event.self, from: Data(line.utf8)) else { continue }
                    switch event.event {
                    case "pair_page":
                        if let path = event.data?.path {
                            await MainActor.run { self?.page = URL(fileURLWithPath: path) }
                        }
                    case "error":
                        lastError = event.data?.message
                    default:
                        break
                    }
                }
            } catch {
                lastError = error.localizedDescription
            }
            process.waitUntilExit()
            let failed = process.terminationStatus != 0
            let message = lastError
            await MainActor.run {
                guard let self, self.process === process else { return }
                self.process = nil
                self.running = false
                if failed { self.failure = message ?? texts.stopped }
            }
        }

        do {
            try process.run()
            self.process = process
        } catch {
            // Nothing will write to the pipe: end the reader.
            try? events.fileHandleForWriting.close()
            running = false
            failure = error.localizedDescription
        }
    }

    /// Ends pairing the way Ctrl-C does, so wacli closes its connection and the store.
    func stop() {
        guard let process, process.isRunning else { return }
        process.interrupt()
        process.waitUntilExit()
    }

    private struct Event: Decodable {
        struct Fields: Decodable {
            let path: String?
            let message: String?
        }
        let event: String
        let data: Fields?
    }
}
