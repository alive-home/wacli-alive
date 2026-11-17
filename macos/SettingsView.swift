import SwiftUI

/// What `wacli push status --json` prints.
struct PushStatus: Decodable, Equatable {
    struct Run: Decodable, Equatable {
        struct Counts: Decodable, Equatable {
            let messages: Int
            let calls: Int
        }
        let at: Date
        let ok: Bool
        let error: String?
        let result: Counts?
    }

    let installed: Bool
    let scheduled: Bool
    let host: String
    let lastRun: Run?

    enum CodingKeys: String, CodingKey {
        case installed, scheduled, host
        case lastRun = "last_run"
    }
}

/// What `wacli push prompt --json` prints.
private struct ReceiverPrompt: Decodable {
    let prompt: String
}

/// Settings: the scheduled push. Off, it asks for the address and the key;
/// on, it says where the copy goes and how the last run ended.
struct SettingsView: View {
    @State private var status: PushStatus?
    @State private var address = ""
    @State private var key = ""
    @State private var working = false
    @State private var failure: String?
    /// Counts the changes made here, so a status read that started before
    /// one cannot put the old state back.
    @State private var changes = 0
    @State private var copied = false

    var body: some View {
        Form {
            Section {
                if let status, status.installed {
                    LabeledContent(texts.sendsTo, value: status.host)
                    LabeledContent(texts.lastRun) { lastRun(status) }
                    if status.scheduled, let run = status.lastRun, !run.ok, let error = run.error {
                        Text(error)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                } else {
                    TextField(texts.address, text: $address, prompt: Text(verbatim: "https://"))
                        .accessibilityIdentifier("push-address")
                    SecureField(texts.key, text: $key)
                        .accessibilityIdentifier("push-key")
                }
            } header: {
                Text(texts.sendTitle)
            } footer: {
                VStack(alignment: .leading, spacing: 12) {
                    Text(texts.sendBody)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if status?.installed == false {
                        Text(texts.noAddress)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    if let failure {
                        Text(failure)
                            .foregroundStyle(.red)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    HStack(spacing: 8) {
                        if status?.installed == false {
                            Button(copied ? texts.copied : texts.copyPrompt) { copyPrompt() }
                                .accessibilityIdentifier("push-prompt")
                        }
                        Spacer()
                        if working { ProgressView().controlSize(.small) }
                        if let status, status.installed {
                            Button(texts.turnOff) { change(["push", "uninstall"]) }
                                .accessibilityIdentifier("push-off")
                        } else {
                            Button(working ? texts.checking : texts.turnOn) { turnOn() }
                            .keyboardShortcut(.defaultAction)
                            .disabled(address.trimmingCharacters(in: .whitespaces).isEmpty)
                            .accessibilityIdentifier("push-on")
                        }
                    }
                    .disabled(working || status == nil)
                }
                .font(.callout)
                .padding(.top, 4)
            }
        }
        .formStyle(.grouped)
        .frame(width: 480)
        .fixedSize(horizontal: false, vertical: true)
        .task {
            // The schedule runs on its own; show its latest run while the window is open.
            while !Task.isCancelled {
                if !working { await refresh() }
                try? await Task.sleep(for: .seconds(20))
            }
        }
    }

    @ViewBuilder private func lastRun(_ status: PushStatus) -> some View {
        if !status.scheduled {
            Text(texts.notScheduled).foregroundStyle(.red)
        } else if let run = status.lastRun {
            let when = run.at.formatted(.relative(presentation: .named))
            if run.ok, let counts = run.result {
                Text(texts.sent(when, counts.messages, counts.calls))
            } else {
                Text(texts.failed(when)).foregroundStyle(.red)
            }
        } else {
            Text(texts.firstRun).foregroundStyle(.secondary)
        }
    }

    private func refresh() async {
        let before = changes
        do {
            let read = try await Wacli.json(PushStatus.self, ["push", "status"])
            if before == changes, !working { status = read }
        } catch let error as Wacli.Failure {
            failure = error.message
        } catch {
            failure = error.localizedDescription
        }
    }

    /// The address and the key go over standard input: a program's arguments
    /// can be read by every other program on the Mac.
    private func turnOn() {
        let input = ["url": address.trimmingCharacters(in: .whitespaces), "key": key]
        guard let json = try? JSONEncoder().encode(input) else { return }
        change(["push", "install", "--stdin"], input: String(decoding: json, as: UTF8.self))
    }

    /// Puts the text for the agent that builds the receiver on the clipboard.
    /// wacli holds the text, so the app and the terminal give the same one.
    private func copyPrompt() {
        Task {
            do {
                let text = try await Wacli.json(ReceiverPrompt.self, ["push", "prompt", "--lang", texts.language])
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(text.prompt, forType: .string)
                copied = true
                try? await Task.sleep(for: .seconds(2))
                copied = false
            } catch let error as Wacli.Failure {
                failure = error.message
            } catch {
                failure = error.localizedDescription
            }
        }
    }

    private func change(_ arguments: [String], input: String? = nil) {
        working = true
        failure = nil
        changes += 1
        Task {
            do {
                status = try await Wacli.json(PushStatus.self, arguments, input: input)
                key = ""
            } catch let error as Wacli.Failure {
                failure = error.message
            } catch {
                failure = error.localizedDescription
            }
            working = false
        }
    }
}
