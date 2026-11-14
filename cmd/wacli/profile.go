package main

import (
	"context"
	"encoding/base64"
	"fmt"
	_ "image/png"
	"os"
	"strings"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/wa"
	"github.com/spf13/cobra"
	"go.mau.fi/whatsmeow/types"
)

func newProfileCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage your WhatsApp profile",
	}
	cmd.AddCommand(newProfilePictureInfoCmd(flags))
	cmd.AddCommand(newProfileGetAboutCmd(flags))
	cmd.AddCommand(newProfileBusinessCmd(flags))
	return cmd
}

func newProfilePictureInfoCmd(flags *rootFlags) *cobra.Command {
	var targetRaw, existingID string
	var preview bool
	cmd := &cobra.Command{
		Use:   "picture-info --jid <jid-or-phone>",
		Short: "Fetch WhatsApp profile picture metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			target, err := parseProfileTarget(targetRaw)
			if err != nil {
				return err
			}
			ctx, cancel, a, lk, err := openLiveProfileApp(flags, true)
			if err != nil {
				return err
			}
			defer cancel()
			defer closeApp(a, lk)
			info, err := a.WA().GetProfilePictureInfo(ctx, target, preview, existingID)
			if err != nil {
				return fmt.Errorf("get profile picture info: %w", err)
			}
			output := formatProfilePictureInfo(target, info)
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, output)
			}
			if info == nil {
				fmt.Fprintf(os.Stdout, "%s profile picture is unchanged.\n", sanitize(target.String()))
				return nil
			}
			fmt.Fprintf(os.Stdout, "JID: %s\nID: %s\nType: %s\nURL: %s\nDirect path: %s\n", sanitize(output.JID), sanitize(output.ID), sanitize(output.Type), sanitize(output.URL), sanitize(output.DirectPath))
			return nil
		},
	}
	cmd.Flags().StringVar(&targetRaw, "jid", "", "target JID or phone number")
	cmd.Flags().BoolVar(&preview, "preview", false, "fetch preview picture metadata")
	cmd.Flags().StringVar(&existingID, "existing-id", "", "last known picture ID; unchanged pictures return null metadata")
	return cmd
}

func newProfileGetAboutCmd(flags *rootFlags) *cobra.Command {
	var targetRaw string
	cmd := &cobra.Command{
		Use:   "get-about --jid <jid-or-phone>",
		Short: "Fetch a user's WhatsApp profile About text",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			target, err := parseProfileTarget(targetRaw)
			if err != nil {
				return err
			}
			ctx, cancel, a, lk, err := openLiveProfileApp(flags, true)
			if err != nil {
				return err
			}
			defer cancel()
			defer closeApp(a, lk)
			output, err := fetchProfileAbout(ctx, a.WA(), target)
			if err != nil {
				return fmt.Errorf("get profile about: %w", err)
			}
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, output)
			}
			fmt.Fprintf(os.Stdout, "JID: %s\nAbout: %s\n", sanitize(output.JID), sanitize(output.About))
			return nil
		},
	}
	cmd.Flags().StringVar(&targetRaw, "jid", "", "target JID or phone number")
	return cmd
}

func newProfileBusinessCmd(flags *rootFlags) *cobra.Command {
	var targetRaw string
	cmd := &cobra.Command{
		Use:   "business --jid <jid-or-phone>",
		Short: "Fetch a WhatsApp business profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			target, err := parseProfileTarget(targetRaw)
			if err != nil {
				return err
			}
			ctx, cancel, a, lk, err := openLiveProfileApp(flags, true)
			if err != nil {
				return err
			}
			defer cancel()
			defer closeApp(a, lk)
			profile, err := a.WA().GetBusinessProfile(ctx, target)
			if err != nil {
				return fmt.Errorf("get business profile: %w", err)
			}
			output := formatBusinessProfile(profile)
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, output)
			}
			fmt.Fprintf(os.Stdout, "JID: %s\nAddress: %s\nEmail: %s\nTimezone: %s\n", sanitize(output.JID), sanitize(output.Address), sanitize(output.Email), sanitize(output.BusinessHoursTimeZone))
			return nil
		},
	}
	cmd.Flags().StringVar(&targetRaw, "jid", "", "target JID or phone number")
	return cmd
}

func openLiveProfileApp(flags *rootFlags, needLock bool) (context.Context, context.CancelFunc, *app.App, *lock.Lock, error) {
	ctx, cancel := withTimeout(context.Background(), flags)
	a, lk, err := newApp(ctx, flags, needLock, false)
	if err != nil {
		cancel()
		return nil, nil, nil, nil, err
	}
	if err := a.EnsureAuthed(ctx); err != nil {
		cancel()
		closeApp(a, lk)
		return nil, nil, nil, nil, err
	}
	if err := a.Connect(ctx, false, nil); err != nil {
		cancel()
		closeApp(a, lk)
		return nil, nil, nil, nil, err
	}
	return ctx, cancel, a, lk, nil
}

func parseProfileTarget(raw string) (types.JID, error) {
	if strings.TrimSpace(raw) == "" {
		return types.JID{}, fmt.Errorf("--jid is required")
	}
	return wa.ParseUserOrJID(raw)
}

type profilePictureInfoOutput struct {
	JID        string `json:"jid"`
	ID         string `json:"id,omitempty"`
	URL        string `json:"url,omitempty"`
	Type       string `json:"type,omitempty"`
	DirectPath string `json:"direct_path,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Unchanged  bool   `json:"unchanged,omitempty"`
}

func formatProfilePictureInfo(jid types.JID, info *types.ProfilePictureInfo) profilePictureInfoOutput {
	output := profilePictureInfoOutput{JID: jid.String()}
	if info == nil {
		output.Unchanged = true
		return output
	}
	output.ID = info.ID
	output.URL = info.URL
	output.Type = info.Type
	output.DirectPath = info.DirectPath
	if len(info.Hash) > 0 {
		output.Hash = base64.StdEncoding.EncodeToString(info.Hash)
	}
	return output
}

type profileAboutOutput struct {
	JID   string `json:"jid"`
	About string `json:"about"`
}

type profileUserInfoGetter interface {
	GetUserInfo(ctx context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error)
}

func fetchProfileAbout(ctx context.Context, client profileUserInfoGetter, target types.JID) (profileAboutOutput, error) {
	infos, err := client.GetUserInfo(ctx, []types.JID{target})
	if err != nil {
		return profileAboutOutput{}, err
	}
	info, ok := infos[target]
	if !ok {
		return profileAboutOutput{}, fmt.Errorf("profile about unavailable for %s", target.String())
	}
	return profileAboutOutput{JID: target.String(), About: info.Status}, nil
}

type businessProfileOutput struct {
	JID                   string                      `json:"jid"`
	Address               string                      `json:"address,omitempty"`
	Email                 string                      `json:"email,omitempty"`
	Categories            []types.Category            `json:"categories,omitempty"`
	ProfileOptions        map[string]string           `json:"profile_options,omitempty"`
	BusinessHoursTimeZone string                      `json:"business_hours_timezone,omitempty"`
	BusinessHours         []types.BusinessHoursConfig `json:"business_hours,omitempty"`
}

func formatBusinessProfile(profile *types.BusinessProfile) businessProfileOutput {
	if profile == nil {
		return businessProfileOutput{}
	}
	return businessProfileOutput{
		JID:                   profile.JID.String(),
		Address:               profile.Address,
		Email:                 profile.Email,
		Categories:            profile.Categories,
		ProfileOptions:        profile.ProfileOptions,
		BusinessHoursTimeZone: profile.BusinessHoursTimeZone,
		BusinessHours:         profile.BusinessHours,
	}
}
