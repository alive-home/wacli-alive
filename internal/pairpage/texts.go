package pairpage

import (
	"html/template"
	"strings"
)

// The languages of the page. English is what anything else falls back to.
const (
	Dutch   = "nl"
	English = "en"
)

// Language is the page language for what was asked: Dutch for "nl" and its
// regions ("nl-BE", "nl_NL"), English for everything else.
func Language(asked string) string {
	asked = strings.ToLower(strings.TrimSpace(asked))
	if asked == Dutch || strings.HasPrefix(asked, Dutch+"-") || strings.HasPrefix(asked, Dutch+"_") {
		return Dutch
	}
	return English
}

// text is every sentence of the page and the prompt in one language.
// page.html is the layout both languages share.
type text struct {
	Title string

	ScanTitle, ScanLead string
	Steps               []template.HTML
	Waiting, Refreshes  string

	LinkedTitle, Syncing  string
	Fetched, CloseWindow  string
	Halted                string
	StoppedTitle, Stopped string

	Read, ReadBody          string
	How, HowBody            string
	Where, WhereIs, WhereTo string
	Check, CheckBody        string

	Assistant, AssistantBody string
	Copy, Copied, Selected   string

	// The page `wacli app` serves adds a panel for the scheduled push. The
	// Mac app says the same in its Settings (macos/Texts.swift).
	StartAgain              string
	SendTitle, SendBody     string
	Address, Key, NoAddress string
	CopyPrompt              string
	TurnOn, Checking        string
	TurnOff                 string
	SendsTo, LastRun        string
	LastRead, OnServer      string
	// Panel is what the panel's script says: sentences it builds from numbers
	// and times.
	Panel map[string]string

	// The prompt: PromptEnd takes the store directory and the command to
	// pair again.
	PromptStart, PromptEnd string
	Relink                 string
}

var texts = map[string]text{
	Dutch: {
		Title:     "WhatsApp koppelen - Alive",
		ScanTitle: "Koppel je WhatsApp",
		ScanLead:  "Scan de code met je telefoon. Deze computer kan daarna alleen lezen, niet versturen.",
		Steps: []template.HTML{
			"Open <b>WhatsApp</b> op je telefoon.",
			"Tik op <b>Instellingen</b>. Android: de <b>drie puntjes</b>.",
			"Tik op <b>Gekoppelde apparaten</b>, dan <b>Apparaat koppelen</b>.",
			"Richt je telefoon op de code.",
		},
		Waiting:   "Code wordt opgehaald…",
		Refreshes: "De code ververst vanzelf.",

		LinkedTitle:  "WhatsApp is gekoppeld",
		Syncing:      "Gelukt. Wacht even tot je berichten binnen zijn. De eerste keer duurt dat een paar minuten.",
		Fetched:      "Opgehaalde berichten:",
		CloseWindow:  "Je kunt dit venster sluiten.",
		Halted:       "Het ophalen is gestopt. Start het koppelen opnieuw; scannen hoeft niet nog een keer.",
		StoppedTitle: "Het koppelen is gestopt",
		Stopped:      "Er komt geen nieuwe code meer. Start het koppelen opnieuw, net als de vorige keer.",

		Read:      "Wat kan het lezen?",
		ReadBody:  "Je chats, groepen, contacten, foto's en bestanden. Versturen, verwijderen of als gelezen markeren kan het niet.",
		How:       "Hoe werkt het?",
		HowBody:   "Deze computer wordt een gekoppeld apparaat en kopieert je berichten. Stoppen: log het uit in WhatsApp, onder Gekoppelde apparaten.",
		Where:     "Waar staan je gegevens?",
		WhereIs:   "Op deze computer:",
		WhereTo:   "Het programma stuurt ze uit zichzelf nergens heen. Een AI-assistent op deze computer kan ze lezen.",
		Check:     "Kun je dat controleren?",
		CheckBody: "Ja. De code is open source:",

		Assistant:     "Voor je AI-assistent",
		AssistantBody: "Plak dit in je assistent, dan weet die hoe het werkt.",
		Copy:          "Kopieer tekst",
		Copied:        "Gekopieerd",
		Selected:      "Tekst geselecteerd. Kopieer met Ctrl+C of ⌘C.",

		StartAgain: "Opnieuw starten",
		SendTitle:  "Stuur een kopie naar een server",
		SendBody:   "Elke 15 minuten haalt deze computer op wat nieuw is op WhatsApp en stuurt elk bericht en elk belletje naar dit adres. Het stuurt alleen: de server kan deze computer niets vragen.",
		Address:    "Adres",
		Key:        "Sleutel",
		NoAddress:  "Nog geen adres? De AI-agent van je eigen project kan de ontvangende kant bouwen. Kopieer de opdracht en plak hem in de chat: je krijgt het adres en de sleutel terug.",
		CopyPrompt: "Kopieer opdracht",
		TurnOn:     "Zet aan",
		Checking:   "Controleren…",
		TurnOff:    "Zet uit",
		SendsTo:    "Stuurt naar",
		LastRun:    "Laatste ronde",
		LastRead:   "Laatst gelezen",
		OnServer:   "Op de server",
		Panel: map[string]string{
			"message": "bericht", "messages": "berichten", "call": "belletje", "calls": "belletjes",
			"firstRun":     "De eerste ronde loopt.",
			"failed":       "Mislukt",
			"notScheduled": "Deze computer start het niet meer. Zet het uit en weer aan.",
			"neverRead":    "Nog niet gelezen",
			"today":        "keer vandaag",
			"copied":       "Gekopieerd",
			"selected":     "Tekst geselecteerd. Kopieer met Ctrl+C of ⌘C.",
			"closed":       "wacli is gesloten. Open wacli opnieuw om verder te gaan.",
		},

		PromptStart: "Mijn WhatsApp is op deze computer gekoppeld met wacli. Het kan alleen lezen, niet versturen.\n\nOpdrachten:\n",
		PromptEnd:   "\nMet --json krijg je uitvoer die je kunt verwerken. De berichten staan in %s.\nNiet meer gekoppeld? Start `%s`; ik scan de code.",
		Relink:      "auth --open --lang nl",
	},
	English: {
		Title:     "Link WhatsApp - Alive",
		ScanTitle: "Link your WhatsApp",
		ScanLead:  "Scan the code with your phone. After that this computer can only read, not send.",
		Steps: []template.HTML{
			"Open <b>WhatsApp</b> on your phone.",
			"Tap <b>Settings</b>. Android: the <b>three dots</b>.",
			"Tap <b>Linked devices</b>, then <b>Link a device</b>.",
			"Point your phone at the code.",
		},
		Waiting:   "Getting a code…",
		Refreshes: "The code refreshes by itself.",

		LinkedTitle:  "WhatsApp is linked",
		Syncing:      "It worked. Wait a moment for your messages to come in. The first time takes a few minutes.",
		Fetched:      "Messages fetched:",
		CloseWindow:  "You can close this window.",
		Halted:       "Fetching stopped. Start linking again; you do not need to scan again.",
		StoppedTitle: "Linking stopped",
		Stopped:      "No new code will appear. Start linking again, the same way as before.",

		Read:      "What can it read?",
		ReadBody:  "Your chats, groups, contacts, photos and files. It cannot send, delete or mark as read.",
		How:       "How does it work?",
		HowBody:   "This computer becomes a linked device and copies your messages. To stop: log it out in WhatsApp, under Linked devices.",
		Where:     "Where is your data?",
		WhereIs:   "On this computer:",
		WhereTo:   "The program sends it nowhere by itself. An AI assistant on this computer can read it.",
		Check:     "Can you check that?",
		CheckBody: "Yes. The code is open source:",

		Assistant:     "For your AI assistant",
		AssistantBody: "Paste this into your assistant so it knows how this works.",
		Copy:          "Copy text",
		Copied:        "Copied",
		Selected:      "Text selected. Copy with Ctrl+C or ⌘C.",

		StartAgain: "Start again",
		SendTitle:  "Send a copy to a server",
		SendBody:   "Every 15 minutes this computer fetches what is new from WhatsApp and posts every message and call to this address. It only sends: the server cannot ask this computer for anything.",
		Address:    "Address",
		Key:        "Key",
		NoAddress:  "No address yet? The AI agent of your own project can build the receiving side. Copy the instructions and paste them in its chat: it answers with the address and the key.",
		CopyPrompt: "Copy instructions",
		TurnOn:     "Turn on",
		Checking:   "Checking…",
		TurnOff:    "Turn off",
		SendsTo:    "Sends to",
		LastRun:    "Last run",
		LastRead:   "Last read",
		OnServer:   "On the server",
		Panel: map[string]string{
			"message": "message", "messages": "messages", "call": "call", "calls": "calls",
			"firstRun":     "The first run is under way.",
			"failed":       "Failed",
			"notScheduled": "This computer no longer starts it. Turn it off and on again.",
			"neverRead":    "Not read yet",
			"today":        "times today",
			"copied":       "Copied",
			"selected":     "Text selected. Copy with Ctrl+C or ⌘C.",
			"closed":       "wacli has closed. Open wacli again to go on.",
		},

		PromptStart: "My WhatsApp is linked on this computer with wacli. It can only read, not send.\n\nCommands:\n",
		PromptEnd:   "\nAdd --json for output you can parse. The messages are in %s.\nNo longer linked? Run `%s` and I will scan the code.",
		Relink:      "auth --open",
	},
}
