package wa

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
)

// whatsmeowCallsAllowed lists every *whatsmeow.Client method this build's
// code may call. They read, decode locally, or carry the protocol traffic
// syncing needs. Only add a name after reading what it puts on the wire.
var whatsmeowCallsAllowed = []string{
	// connection and events
	"AddEventHandler", "RemoveEventHandler", "Connect", "ConnectContext", "Disconnect", "IsConnected",
	"GetQRChannel",
	// pairing this device by phone code instead of QR
	"PairPhone",
	// reads
	"GetUserInfo", "IsOnWhatsApp", "GetJoinedGroups", "GetGroupInfo", "GetGroupInviteLink",
	"GetGroupRequestParticipants", "GetNewsletterInfo", "GetNewsletterInfoWithInvite",
	"GetSubscribedNewsletters", "GetProfilePictureInfo", "GetBusinessProfile",
	// local decoding
	"DecryptPollVote", "DecryptReaction", "DecryptSecretEncryptedMessage", "ParseWebMessage",
	"DangerousInternals",
	// downloads and history
	"DownloadMediaWithPathToFile", "DownloadHistorySync", "DeleteMedia", "BuildHistorySyncRequest",
	"FetchAppState",
	// requests to the account's own phone, see whatsmeowSendsAllowed
	"SendMessage", "SendPeerMessage", "SendMediaRetryReceipt",
}

// whatsmeowSendsAllowed pins each remaining send to the places it is used.
// All three go to the account's own phone and show up in no chat: a history
// request, an app-state recovery request, and a request to upload media again
// (which the app reaches through its own wrapper of the same name).
var whatsmeowSendsAllowed = map[string][]string{
	"SendMessage":           {"internal/wa/client.go"},
	"SendPeerMessage":       {"internal/wa/client.go"},
	"SendMediaRetryReceipt": {"internal/app/guard.go", "internal/app/media_retry.go", "internal/wa/mediaretry.go"},
}

// The code of this build calls no whatsmeow method that sends to or changes
// WhatsApp. It finds every call in non-test code whose name is a method of
// *whatsmeow.Client, so code a merge brings in fails here until it is read.
func TestBuildCallsOnlyAllowedWhatsmeowMethods(t *testing.T) {
	clientType := reflect.TypeOf(&whatsmeow.Client{})
	for _, name := range whatsmeowCallsAllowed {
		if _, ok := clientType.MethodByName(name); !ok {
			t.Errorf("allowed name %s is not a *whatsmeow.Client method; remove it", name)
		}
	}

	root := filepath.Join("..", "..")
	sends := map[string][]string{}
	called := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				if _, isClientMethod := clientType.MethodByName(name); !isClientMethod {
					return true
				}
				called[name] = true
				if !slices.Contains(whatsmeowCallsAllowed, name) {
					t.Errorf("%s: %s is a *whatsmeow.Client method this build does not allow", fset.Position(sel.Pos()), name)
				}
				if _, isSend := whatsmeowSendsAllowed[name]; isSend {
					sends[name] = append(sends[name], rel)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, files := range whatsmeowSendsAllowed {
		slices.Sort(sends[name])
		if !slices.Equal(sends[name], files) {
			t.Errorf("%s is used in %v, want once in each of %v", name, sends[name], files)
		}
	}
	for _, name := range whatsmeowCallsAllowed {
		if !called[name] {
			t.Errorf("allowed name %s is not called anywhere; remove it", name)
		}
	}
}
