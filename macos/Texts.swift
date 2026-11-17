import Foundation

/// Every sentence of the app in one language. The pairing page has its own
/// (internal/pairpage/texts.go) and gets the same language.
struct Texts {
    let language: String

    let moveTitle, moveBody, quit: String
    let startAgain, stopped, couldNotStart: String

    let sendTitle, sendBody: String
    let address, key: String
    let noAddress, copyPrompt, copied: String
    let turnOn, checking, turnOff: String
    let sendsTo, lastRun, firstRun, notScheduled: String
    let sent: (_ when: String, _ messages: Int, _ calls: Int) -> String
    let failed: (_ when: String) -> String
}

let texts: Texts = Locale.preferredLanguages.first?.hasPrefix("nl") == true ? .dutch : .english

extension Texts {
    static let english = Texts(
        language: "en",
        moveTitle: "Move wacli to Applications first",
        moveBody: "Drag wacli to the Applications folder and open it from there.",
        quit: "Quit",
        startAgain: "Start again",
        stopped: "Linking stopped.",
        couldNotStart: "Linking could not start",
        sendTitle: "Send a copy to a server",
        sendBody: "Every 15 minutes this Mac fetches what is new from WhatsApp and posts every message and call to this address. It only sends: the server cannot ask this Mac for anything.",
        address: "Address",
        key: "Key",
        noAddress: "No address yet? The AI agent of your own project can build the receiving side. Copy the instructions and paste them in its chat: it answers with the address and the key.",
        copyPrompt: "Copy instructions",
        copied: "Copied",
        turnOn: "Turn on",
        checking: "Checking…",
        turnOff: "Turn off",
        sendsTo: "Sends to",
        lastRun: "Last run",
        firstRun: "The first run is under way.",
        notScheduled: "macOS no longer starts it. Turn it off and on again.",
        sent: { when, messages, calls in "\(when): \(messages) \(messages == 1 ? "message" : "messages"), \(calls) \(calls == 1 ? "call" : "calls")" },
        failed: { when in "Failed \(when)" }
    )

    static let dutch = Texts(
        language: "nl",
        moveTitle: "Zet wacli eerst in Programma's",
        moveBody: "Sleep wacli naar de map Programma's en open het daar.",
        quit: "Stop",
        startAgain: "Opnieuw starten",
        stopped: "Het koppelen is gestopt.",
        couldNotStart: "Het koppelen kon niet starten",
        sendTitle: "Stuur een kopie naar een server",
        sendBody: "Elke 15 minuten haalt deze Mac op wat nieuw is op WhatsApp en stuurt elk bericht en elk belletje naar dit adres. Het stuurt alleen: de server kan deze Mac niets vragen.",
        address: "Adres",
        key: "Sleutel",
        noAddress: "Nog geen adres? De AI-agent van je eigen project kan de ontvangende kant bouwen. Kopieer de opdracht en plak hem in de chat: je krijgt het adres en de sleutel terug.",
        copyPrompt: "Kopieer opdracht",
        copied: "Gekopieerd",
        turnOn: "Zet aan",
        checking: "Controleren…",
        turnOff: "Zet uit",
        sendsTo: "Stuurt naar",
        lastRun: "Laatste ronde",
        firstRun: "De eerste ronde loopt.",
        notScheduled: "macOS start het niet meer. Zet het uit en weer aan.",
        sent: { when, messages, calls in "\(when): \(messages) \(messages == 1 ? "bericht" : "berichten"), \(calls) \(calls == 1 ? "belletje" : "belletjes")" },
        failed: { when in "Mislukt \(when)" }
    )
}
