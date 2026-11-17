package main

import (
	"fmt"
	"os"

	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/push"
	"github.com/spf13/cobra"
)

// newPushPromptCmd prints the text for the agent that builds the receiver. It
// opens no store and reaches nothing.
func newPushPromptCmd(flags *rootFlags) *cobra.Command {
	var lang string
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Print the instructions for the AI agent that builds your receiver",
		Long: "prompt prints a text to paste into the AI agent of your own project. The agent builds\n" +
			"the receiving side of `wacli push` there and answers with the address and the key that\n" +
			"`wacli push install` asks for.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			answerIn := ""
			if lang == "nl" {
				answerIn = "Dutch"
			}
			text := push.ReceiverPrompt(answerIn)
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"prompt": text})
			}
			_, err := fmt.Fprint(os.Stdout, text)
			return err
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "en", "language the agent answers you in: nl or en")
	return cmd
}
