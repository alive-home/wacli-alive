package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/store"
	"github.com/spf13/cobra"
	"go.mau.fi/whatsmeow/types"
)

func newPollCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Vote on or inspect a poll",
	}
	cmd.AddCommand(newPollShowCmd(flags))
	return cmd
}

func newPollsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "polls",
		Short: "List polls",
	}
	cmd.AddCommand(newPollsListCmd(flags))
	return cmd
}

// ---- vote -----------------------------------------------------------------

type pollVoteApp interface {
	DB() *store.DB
	WA() app.WAClient
	StoreDir() string
	LocalResolver() (app.LocalResolver, error)
	EnsureAuthed(context.Context) error
	Connect(context.Context, bool, func(string)) error
}

func pollChatJIDCandidates(ctx context.Context, a pollVoteApp, jid types.JID) []string {
	jids := make([]types.JID, 0, 3)
	if a != nil {
		if _, err := os.Stat(filepath.Join(a.StoreDir(), "session.db")); err == nil {
			resolver, _ := a.LocalResolver()
			if resolver != nil {
				resolved := resolver.ResolveLIDToPN(ctx, jid)
				jids = append(jids, canonicalMessageFilterJID(resolved))
				if resolved.Server == types.DefaultUserServer {
					jids = append(jids, canonicalMessageFilterJID(resolver.ResolvePNToLID(ctx, resolved)))
				}
			}
		}
	}
	jids = append(jids, canonicalMessageFilterJID(jid))
	return jidStrings(jids)
}

// ---- show -----------------------------------------------------------------

func newPollShowCmd(flags *rootFlags) *cobra.Command {
	var to string
	var pick int
	var msgID string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a poll's question, options, and votes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(to) == "" || strings.TrimSpace(msgID) == "" {
				return fmt.Errorf("--to and --id are required")
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, false, true)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			toJID, err := resolveRecipient(a, to, recipientOptions{pick: pick, asJSON: flags.asJSON})
			if err != nil {
				return err
			}
			poll, votes, err := getPollForShow(ctx, a, toJID, msgID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("poll %s not found in local store for chat %s; run `wacli sync` first", msgID, toJID.String())
				}
				return err
			}

			aggregates := aggregatePollVotes(poll.Options, votes)

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, pollShowPayload(a, ctx, poll, votes, aggregates))
			}
			renderPollShow(os.Stdout, a, ctx, poll, votes, aggregates)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "chat JID, phone number, or contact/group/chat name")
	cmd.Flags().IntVar(&pick, "pick", 0, "when --to is ambiguous, pick the Nth match (1-indexed)")
	cmd.Flags().StringVar(&msgID, "id", "", "poll message ID")
	return cmd
}

func getPollForShow(ctx context.Context, a *app.App, chat types.JID, msgID string) (store.Poll, []store.PollVote, error) {
	var lastErr error = sql.ErrNoRows
	for _, chatJID := range pollChatJIDCandidates(ctx, a, chat) {
		poll, err := a.DB().GetPoll(chatJID, msgID)
		if err != nil {
			lastErr = err
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return store.Poll{}, nil, err
		}
		votes, err := a.DB().ListPollVotes(poll.ChatJID, msgID)
		if err != nil {
			return store.Poll{}, nil, err
		}
		return poll, votes, nil
	}
	return store.Poll{}, nil, lastErr
}

func aggregatePollVotes(options []string, votes []store.PollVote) map[string]int {
	out := make(map[string]int, len(options))
	for _, o := range options {
		out[o] = 0
	}
	for _, v := range votes {
		for _, sel := range v.Selected {
			out[sel]++
		}
	}
	return out
}

type pollVoterPayload struct {
	JID           string   `json:"jid"`
	Name          string   `json:"name,omitempty"`
	Selected      []string `json:"selected"`
	UnknownHashes []string `json:"unknown_hashes,omitempty"`
	VotedAt       string   `json:"voted_at"`
}

type pollShowJSON struct {
	Question        string             `json:"question"`
	Options         []string           `json:"options"`
	SelectableCount uint32             `json:"selectable_count"`
	ChatJID         string             `json:"chat_jid"`
	MsgID           string             `json:"msg_id"`
	SenderJID       string             `json:"sender_jid,omitempty"`
	CreatedAt       string             `json:"created_at"`
	Aggregates      map[string]int     `json:"aggregates"`
	Voters          []pollVoterPayload `json:"voters"`
}

func pollShowPayload(a *app.App, ctx context.Context, poll store.Poll, votes []store.PollVote, aggregates map[string]int) pollShowJSON {
	out := pollShowJSON{
		Question:        poll.Question,
		Options:         poll.Options,
		SelectableCount: poll.SelectableCount,
		ChatJID:         poll.ChatJID,
		MsgID:           poll.MsgID,
		SenderJID:       poll.SenderJID,
		CreatedAt:       poll.CreatedAt.Format(time.RFC3339),
		Aggregates:      aggregates,
		Voters:          make([]pollVoterPayload, 0, len(votes)),
	}
	for _, v := range votes {
		name := resolveVoterName(a, ctx, v.VoterJID)
		out.Voters = append(out.Voters, pollVoterPayload{
			JID:           v.VoterJID,
			Name:          name,
			Selected:      v.Selected,
			UnknownHashes: v.UnknownHashes,
			VotedAt:       v.VotedAt.Format(time.RFC3339),
		})
	}
	return out
}

func resolveVoterName(a *app.App, ctx context.Context, jid string) string {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	if a != nil && a.DB() != nil {
		if c, err := a.DB().GetContact(jid); err == nil {
			if name := strings.TrimSpace(c.Name); name != "" {
				return name
			}
		}
	}
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return ""
	}
	if a == nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(a.StoreDir(), "session.db")); err != nil {
		return ""
	}
	resolver, err := a.LocalResolver()
	if err != nil {
		return ""
	}
	return resolver.ResolveChatName(ctx, parsed, "")
}

func renderPollShow(w *os.File, a *app.App, ctx context.Context, poll store.Poll, votes []store.PollVote, aggregates map[string]int) {
	fmt.Fprintf(w, "Poll: %s\n", sanitize(poll.Question))
	fmt.Fprintf(w, "Chat: %s   Msg: %s   Selectable: %d\n", sanitize(poll.ChatJID), sanitize(poll.MsgID), poll.SelectableCount)
	fmt.Fprintf(w, "\nResults (%d voter(s)):\n", len(votes))
	for _, opt := range poll.Options {
		fmt.Fprintf(w, "  %3d  %s\n", aggregates[opt], sanitize(opt))
	}
	if len(votes) == 0 {
		fmt.Fprintln(w, "\nNo votes yet.")
		return
	}
	fmt.Fprintln(w, "\nVoters:")
	for _, v := range votes {
		name := resolveVoterName(a, ctx, v.VoterJID)
		who := v.VoterJID
		if name != "" && name != v.VoterJID {
			who = fmt.Sprintf("%s (%s)", name, v.VoterJID)
		}
		selected := make([]string, 0, len(v.Selected))
		for _, option := range v.Selected {
			selected = append(selected, sanitize(option))
		}
		fmt.Fprintf(w, "  %s — %s [%s]\n", sanitize(who), strings.Join(selected, ", "), v.VotedAt.Format(time.RFC3339))
	}
}

// ---- list -----------------------------------------------------------------

func newPollsListCmd(flags *rootFlags) *cobra.Command {
	var chat string
	var pick int
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List polls stored locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, false, true)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			filter := store.PollListFilter{Limit: limit}
			if strings.TrimSpace(chat) != "" {
				toJID, err := resolveRecipient(a, chat, recipientOptions{pick: pick, asJSON: flags.asJSON})
				if err != nil {
					return err
				}
				filter.ChatJIDs = pollChatJIDCandidates(ctx, a, toJID)
			}
			polls, err := a.DB().ListPolls(filter)
			if err != nil {
				return err
			}

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"polls": pollsToJSON(polls)})
			}
			renderPollsList(os.Stdout, polls)
			return nil
		},
	}

	cmd.Flags().StringVar(&chat, "chat", "", "filter to a single chat")
	cmd.Flags().IntVar(&pick, "pick", 0, "when --chat is ambiguous, pick the Nth match (1-indexed)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max polls to return (0 = unlimited)")
	return cmd
}

type pollListItemJSON struct {
	ChatJID         string   `json:"chat_jid"`
	MsgID           string   `json:"msg_id"`
	Question        string   `json:"question"`
	Options         []string `json:"options"`
	SelectableCount uint32   `json:"selectable_count"`
	SenderJID       string   `json:"sender_jid,omitempty"`
	CreatedAt       string   `json:"created_at"`
}

func pollsToJSON(polls []store.Poll) []pollListItemJSON {
	out := make([]pollListItemJSON, 0, len(polls))
	for _, p := range polls {
		out = append(out, pollListItemJSON{
			ChatJID:         p.ChatJID,
			MsgID:           p.MsgID,
			Question:        p.Question,
			Options:         p.Options,
			SelectableCount: p.SelectableCount,
			SenderJID:       p.SenderJID,
			CreatedAt:       p.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func renderPollsList(w *os.File, polls []store.Poll) {
	if len(polls) == 0 {
		fmt.Fprintln(w, "No polls.")
		return
	}
	for _, p := range polls {
		fmt.Fprintf(w, "%s  %s\n  chat=%s  options=%d  selectable=%d  id=%s\n",
			p.CreatedAt.Format(time.RFC3339),
			sanitize(p.Question),
			sanitize(p.ChatJID),
			len(p.Options),
			p.SelectableCount,
			sanitize(p.MsgID),
		)
	}
}
