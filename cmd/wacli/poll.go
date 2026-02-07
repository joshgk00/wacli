package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steipete/wacli/internal/app"
	"github.com/steipete/wacli/internal/out"
	"github.com/steipete/wacli/internal/store"
)

func newPollCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Poll operations",
	}
	cmd.AddCommand(newPollResultsCmd(flags))
	cmd.AddCommand(newPollListCmd(flags))
	return cmd
}

func newPollResultsCmd(flags *rootFlags) *cobra.Command {
	var chatJID string
	var msgID string

	cmd := &cobra.Command{
		Use:   "results",
		Short: "Show poll results",
		Long:  "Display results for a poll including vote counts and voter details",
		Example: `  wacli poll results --chat 123456@g.us --id ABCD1234EFGH5678
  wacli poll results --chat 1234567890@s.whatsapp.net --id XYZ123 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if chatJID == "" || msgID == "" {
				return fmt.Errorf("--chat and --id are required")
			}

			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, false, false)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			results, err := a.DB().GetPollResults(chatJID, msgID)
			if err != nil {
				if store.IsNotFound(err) {
					return fmt.Errorf("poll not found (chat: %s, id: %s)", chatJID, msgID)
				}
				return err
			}

			// Resolve voter names from contacts
			for i := range results.Votes {
				if results.Votes[i].VoterName == "" {
					results.Votes[i].VoterName = resolveVoterName(a, results.Votes[i].VoterJID)
				}
			}

			return displayPollResults(results, flags.asJSON)
		},
	}

	cmd.Flags().StringVar(&chatJID, "chat", "", "Chat JID where poll was sent")
	cmd.Flags().StringVar(&msgID, "id", "", "Poll message ID")
	cmd.MarkFlagRequired("chat")
	cmd.MarkFlagRequired("id")

	return cmd
}

func newPollListCmd(flags *rootFlags) *cobra.Command {
	var chatJID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tracked polls",
		Long:  "Display all polls being tracked in the database",
		Example: `  wacli poll list
  wacli poll list --chat 123456@g.us
  wacli poll list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, false, false)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			polls, err := a.DB().ListPolls(chatJID)
			if err != nil {
				return err
			}

			return displayPollList(polls, flags.asJSON)
		},
	}

	cmd.Flags().StringVar(&chatJID, "chat", "", "Filter by chat JID (optional)")

	return cmd
}

func displayPollList(polls []store.PollListItem, asJSON bool) error {
	if asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"polls": polls,
			"count": len(polls),
		})
	}

	if len(polls) == 0 {
		fmt.Fprintln(os.Stdout, "No polls found.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "\nFound %d poll(s):\n\n", len(polls))
	for i, p := range polls {
		fmt.Fprintf(os.Stdout, "[%d] %s\n", i+1, p.Question)
		fmt.Fprintf(os.Stdout, "    Chat: %s\n", p.ChatJID)
		fmt.Fprintf(os.Stdout, "    Message ID: %s\n", p.MsgID)
		fmt.Fprintf(os.Stdout, "    Created: %s\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(os.Stdout, "    Votes: %d\n", p.VoteCount)
		fmt.Fprintln(os.Stdout)
	}

	return nil
}

func resolveVoterName(a *app.App, voterJID string) string {
	if strings.TrimSpace(voterJID) == "" {
		return ""
	}
	contact, err := a.DB().GetContact(voterJID)
	if err == nil && contact.Name != "" {
		if contact.Alias != "" {
			return contact.Alias
		}
		return contact.Name
	}
	return voterJID
}

func displayPollResults(results store.PollResults, asJSON bool) error {
	if asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"question":         results.Question,
			"selectable_count": results.SelectableCount,
			"created_at":       results.CreatedAt.Unix(),
			"options":          results.Options,
			"votes":            results.Votes,
			"total_voters":     len(results.Votes),
		})
	}

	// Human-readable output
	fmt.Fprintf(os.Stdout, "\nPoll: %s\n", results.Question)
	fmt.Fprintf(os.Stdout, "Created: %s\n", results.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(os.Stdout, "Max selections: %d\n", results.SelectableCount)
	fmt.Fprintf(os.Stdout, "Total voters: %d\n\n", len(results.Votes))

	// Display options with vote counts
	fmt.Fprintln(os.Stdout, "Results:")
	for _, opt := range results.Options {
		percentage := 0.0
		if len(results.Votes) > 0 {
			percentage = float64(opt.VoteCount) / float64(len(results.Votes)) * 100
		}
		bar := makeBar(opt.VoteCount, len(results.Votes), 20)
		fmt.Fprintf(os.Stdout, "  [%d] %s\n", opt.Index+1, opt.Name)
		fmt.Fprintf(os.Stdout, "      %s %.1f%% (%d votes)\n", bar, percentage, opt.VoteCount)
	}

	// Display individual votes
	if len(results.Votes) > 0 {
		fmt.Fprintln(os.Stdout, "\nVotes:")
		for _, vote := range results.Votes {
			voterDisplay := vote.VoterName
			if voterDisplay == "" {
				voterDisplay = vote.VoterJID
			}
			optNames := []string{}
			for _, idx := range vote.SelectedIndices {
				if idx >= 0 && idx < len(results.Options) {
					optNames = append(optNames, results.Options[idx].Name)
				}
			}
			fmt.Fprintf(os.Stdout, "  %s: %s (%s)\n",
				voterDisplay,
				strings.Join(optNames, ", "),
				vote.VotedAt.Format("15:04:05"))
		}
	}

	return nil
}

func makeBar(count, total, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 || count <= 0 {
		return strings.Repeat("░", width)
	}
	filled := int(float64(count) / float64(total) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
