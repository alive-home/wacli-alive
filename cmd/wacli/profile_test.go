package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"go.mau.fi/whatsmeow/types"
)

func TestProfileReadCommandsExposeTargetFlags(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "picture-info", cmd: newProfilePictureInfoCmd(&rootFlags{})},
		{name: "get-about", cmd: newProfileGetAboutCmd(&rootFlags{})},
		{name: "business", cmd: newProfileBusinessCmd(&rootFlags{})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd.Flags().Lookup("jid") == nil {
				t.Fatalf("missing --jid flag")
			}
		})
	}
}

func TestProfilePictureInfoJSONOmitsHashBytesWhenEmpty(t *testing.T) {
	info := profilePictureInfoOutput{
		JID:        "15551234567@s.whatsapp.net",
		ID:         "abc",
		URL:        "https://example.invalid/avatar.jpg",
		Type:       "image",
		DirectPath: "/mms/path",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(data, []byte("hash")) {
		t.Fatalf("empty hash should be omitted: %s", data)
	}
}

func TestFetchProfileAboutReturnsTargetStatus(t *testing.T) {
	target := types.NewJID("15551234567", types.DefaultUserServer)
	client := fakeProfileClient{
		userInfo: map[types.JID]types.UserInfo{
			target: {Status: "Available"},
		},
	}
	got, err := fetchProfileAbout(context.Background(), client, target)
	if err != nil {
		t.Fatalf("fetchProfileAbout: %v", err)
	}
	if got.JID != target.String() || got.About != "Available" {
		t.Fatalf("unexpected output: %+v", got)
	}
}

func TestFetchProfileAboutErrorsWhenTargetMissing(t *testing.T) {
	target := types.NewJID("15551234567", types.DefaultUserServer)
	_, err := fetchProfileAbout(context.Background(), fakeProfileClient{userInfo: map[types.JID]types.UserInfo{}}, target)
	if err == nil {
		t.Fatal("expected missing target error")
	}
}

type fakeProfileClient struct {
	userInfo map[types.JID]types.UserInfo
}

func (f fakeProfileClient) GetUserInfo(_ context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error) {
	if len(jids) != 1 {
		return nil, nil
	}
	return f.userInfo, nil
}
