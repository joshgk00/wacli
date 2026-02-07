package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steipete/wacli/internal/out"
	"github.com/steipete/wacli/internal/store"
	"github.com/steipete/wacli/internal/wa"
)

func newSendCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send messages",
	}
	cmd.AddCommand(newSendTextCmd(flags))
	cmd.AddCommand(newSendFileCmd(flags))
	cmd.AddCommand(newSendPollCmd(flags))
	return cmd
}

func newSendTextCmd(flags *rootFlags) *cobra.Command {
	var to string
	var message string

	cmd := &cobra.Command{
		Use:   "text",
		Short: "Send a text message",
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" || message == "" {
				return fmt.Errorf("--to and --message are required")
			}

			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, true, false)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			if err := a.EnsureAuthed(); err != nil {
				return err
			}
			if err := a.Connect(ctx, false, nil); err != nil {
				return err
			}

			toJID, err := wa.ParseUserOrJID(to)
			if err != nil {
				return err
			}

			msgID, err := a.WA().SendText(ctx, toJID, message)
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			chat := toJID
			chatName := a.WA().ResolveChatName(ctx, chat, "")
			kind := chatKindFromJID(chat)
			_ = a.DB().UpsertChat(chat.String(), kind, chatName, now)
			_ = a.DB().UpsertMessage(store.UpsertMessageParams{
				ChatJID:    chat.String(),
				ChatName:   chatName,
				MsgID:      string(msgID),
				SenderJID:  "",
				SenderName: "me",
				Timestamp:  now,
				FromMe:     true,
				Text:       message,
			})

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{
					"sent": true,
					"to":   chat.String(),
					"id":   msgID,
				})
			}
			fmt.Fprintf(os.Stdout, "Sent to %s (id %s)\n", chat.String(), msgID)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "recipient phone number or JID")
	cmd.Flags().StringVar(&message, "message", "", "message text")
	return cmd
}

func newSendPollCmd(flags *rootFlags) *cobra.Command {
	var to string
	var question string
	var optionsRaw string
	var maxSelectable int

	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Send a poll",
		Long:  "Send a WhatsApp poll with a question and options. Options are comma-separated.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" || question == "" || optionsRaw == "" {
				return fmt.Errorf("--to, --question, and --options are required")
			}

			options := strings.Split(optionsRaw, ",")
			for i := range options {
				options[i] = strings.TrimSpace(options[i])
			}
			if len(options) < 2 {
				return fmt.Errorf("at least 2 options are required (comma-separated)")
			}

			if maxSelectable <= 0 {
				maxSelectable = 1
			}

			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, true, false)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			if err := a.EnsureAuthed(); err != nil {
				return err
			}
			if err := a.Connect(ctx, false, nil); err != nil {
				return err
			}

			toJID, err := wa.ParseUserOrJID(to)
			if err != nil {
				return err
			}

			msgID, err := a.WA().SendPoll(ctx, toJID, question, options, maxSelectable)
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			chat := toJID
			chatName := a.WA().ResolveChatName(ctx, chat, "")
			kind := chatKindFromJID(chat)
			_ = a.DB().UpsertChat(chat.String(), kind, chatName, now)
			pollText := fmt.Sprintf("[Poll] %s (%s)", question, strings.Join(options, " | "))
			_ = a.DB().UpsertMessage(store.UpsertMessageParams{
				ChatJID:    chat.String(),
				ChatName:   chatName,
				MsgID:      string(msgID),
				SenderJID:  "",
				SenderName: "me",
				Timestamp:  now,
				FromMe:     true,
				Text:       pollText,
			})

			// Store poll metadata
			_ = a.DB().UpsertPoll(chat.String(), string(msgID), question, maxSelectable, now)
			for i, opt := range options {
				hash := wa.HashPollOption(opt)
				_ = a.DB().UpsertPollOption(chat.String(), string(msgID), i, opt, hash)
			}

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{
					"sent":     true,
					"to":       chat.String(),
					"id":       msgID,
					"question": question,
					"options":  options,
				})
			}
			fmt.Fprintf(os.Stdout, "Poll sent to %s (id %s)\n", chat.String(), msgID)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "recipient phone number or JID")
	cmd.Flags().StringVar(&question, "question", "", "poll question")
	cmd.Flags().StringVar(&optionsRaw, "options", "", "comma-separated poll options")
	cmd.Flags().IntVar(&maxSelectable, "max-selectable", 1, "max options a voter can select (0 = unlimited)")
	return cmd
}
