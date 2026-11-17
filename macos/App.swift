// The Mac app around wacli: one window that links WhatsApp, and a Settings
// window that turns the scheduled push on and off. It holds no logic of its
// own: the window shows the pairing page `wacli auth --page` writes, and
// Settings calls `wacli push install`, `uninstall` and `status`. The wacli
// program is in the bundle at Contents/Helpers/wacli.
import SwiftUI

@main
struct WacliApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        Window("wacli", id: "link") {
            LinkView(linking: delegate.linking)
                .frame(minWidth: 420, minHeight: 480)
        }
        .defaultSize(width: 960, height: 800)
        .commands { CommandGroup(replacing: .newItem) {} }

        Settings { SettingsView() }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let linking = Linking()

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Writing to a wacli that has already ended is an error to handle, not a reason to quit.
        signal(SIGPIPE, SIG_IGN)
        // The scheduled push and the assistant's commands name this program
        // by its path, and an app opened from its disk image or its download
        // runs from a path that is gone after a restart.
        guard installed else {
            let alert = NSAlert()
            alert.messageText = texts.moveTitle
            alert.informativeText = texts.moveBody
            alert.addButton(withTitle: texts.quit)
            alert.runModal()
            NSApp.terminate(nil)
            return
        }
        linking.start()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ notification: Notification) { linking.stop() }

    private var installed: Bool {
        let path = Bundle.main.bundlePath
        return path.hasPrefix("/Applications/") || path.hasPrefix(NSHomeDirectory() + "/Applications/")
    }
}
