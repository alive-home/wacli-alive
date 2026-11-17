import SwiftUI
import WebKit

/// The window: the pairing page, and a way to start again when pairing ended early.
struct LinkView: View {
    @ObservedObject var linking: Linking

    var body: some View {
        VStack(spacing: 0) {
            if let page = linking.page {
                PairingPage(url: page)
                if let failure = linking.failure {
                    // The page says that it stopped; this is why, and the way on.
                    Divider()
                    HStack(spacing: 16) {
                        Text(failure)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .lineLimit(3)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        startAgain
                    }
                    .padding(.horizontal, 20)
                    .padding(.vertical, 12)
                }
            } else if let failure = linking.failure {
                VStack(spacing: 8) {
                    Text(texts.couldNotStart).font(.title3.weight(.medium))
                    Text(failure)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .textSelection(.enabled)
                    startAgain.padding(.top, 12)
                }
                .frame(maxWidth: 440)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Color(nsColor: .pageBackground))
        .background(WindowLook())
    }
}

private extension LinkView {
    var startAgain: some View {
        Button(texts.startAgain) { linking.start() }
            .keyboardShortcut(.defaultAction)
            .accessibilityIdentifier("start-again")
    }
}

extension NSColor {
    /// The pairing page's own background (`--screen` in page.html), so the
    /// title bar and the page are one surface.
    static let pageBackground = NSColor(name: nil) { appearance in
        appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            ? NSColor(srgbRed: 12 / 255, green: 12 / 255, blue: 14 / 255, alpha: 1)
            : NSColor(srgbRed: 0xEF / 255, green: 0xEF / 255, blue: 0xF1 / 255, alpha: 1)
    }
}

/// Gives the window the page's background under a title bar without a title.
private struct WindowLook: NSViewRepresentable {
    final class View: NSView {
        override func viewDidMoveToWindow() {
            guard let window else { return }
            window.titlebarAppearsTransparent = true
            window.titleVisibility = .hidden
            window.backgroundColor = .pageBackground
        }
    }

    func makeNSView(context: Context) -> View { View() }
    func updateNSView(_ view: View, context: Context) {}
}

/// Shows the pairing page. It may read the page's own folder and nothing
/// else of the store; the page loads nothing from the network, and a link in
/// it opens in the browser.
private struct PairingPage: NSViewRepresentable {
    let url: URL

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        // A page opened from a file has no clipboard of its own; its Copy
        // button gets the Mac's.
        configuration.userContentController.add(context.coordinator, name: "copy")
        configuration.userContentController.addUserScript(WKUserScript(
            source: """
                Object.defineProperty(navigator, 'clipboard', { value: { writeText: function (text) {
                  window.webkit.messageHandlers.copy.postMessage(String(text));
                  return Promise.resolve();
                } } });
                """,
            injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = context.coordinator
        view.uiDelegate = context.coordinator
        view.underPageBackgroundColor = .pageBackground
        view.setValue(false, forKey: "drawsBackground")
        return view
    }

    func updateNSView(_ view: WKWebView, context: Context) {
        guard context.coordinator.shown != url else { return }
        context.coordinator.shown = url
        view.loadFileURL(url, allowingReadAccessTo: url.deletingLastPathComponent())
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
        var shown: URL?

        func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
            guard let text = message.body as? String else { return }
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
        }

        func webView(_ view: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
            guard let target = action.request.url else { return .cancel }
            if target.isFileURL, target.path == shown?.path { return .allow }
            openInBrowser(target)
            return .cancel
        }

        // A link that asks for a new window.
        func webView(_ view: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
            if let target = action.request.url { openInBrowser(target) }
            return nil
        }

        private func openInBrowser(_ target: URL) {
            guard target.scheme == "https" else { return }
            NSWorkspace.shared.open(target)
        }
    }
}
