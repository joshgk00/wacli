package wa

import (
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestParsePollCreation(t *testing.T) {
	chat, _ := types.ParseJID("123@g.us")
	sender, _ := types.ParseJID("sender@s.whatsapp.net")

	pollMsg := &waProto.PollCreationMessage{
		Name: proto.String("What's your favorite color?"),
		Options: []*waProto.PollCreationMessage_Option{
			{OptionName: proto.String("Red")},
			{OptionName: proto.String("Blue")},
			{OptionName: proto.String("Green")},
		},
		SelectableOptionsCount: proto.Uint32(1),
	}

	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chat,
				Sender:   sender,
				IsFromMe: false,
			},
			ID:        "poll-msg-1",
			Timestamp: time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC),
			PushName:  "Sender",
		},
		Message: &waProto.Message{PollCreationMessage: pollMsg},
	}

	pm := ParseLiveMessage(ev)

	// Verify poll was extracted
	if pm.Poll == nil {
		t.Fatalf("expected Poll to be set")
	}
	if pm.Poll.Question != "What's your favorite color?" {
		t.Fatalf("unexpected question: %q", pm.Poll.Question)
	}
	if len(pm.Poll.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(pm.Poll.Options))
	}
	if pm.Poll.Options[0] != "Red" || pm.Poll.Options[1] != "Blue" || pm.Poll.Options[2] != "Green" {
		t.Fatalf("unexpected options: %v", pm.Poll.Options)
	}
	if pm.Poll.SelectableCount != 1 {
		t.Fatalf("expected SelectableCount 1, got %d", pm.Poll.SelectableCount)
	}

	// Verify display text includes [Poll] prefix
	if pm.Text != "[Poll] What's your favorite color?" {
		t.Fatalf("unexpected display text: %q", pm.Text)
	}
}

func TestParsePollCreationMultiSelect(t *testing.T) {
	chat, _ := types.ParseJID("123@g.us")
	sender, _ := types.ParseJID("sender@s.whatsapp.net")

	pollMsg := &waProto.PollCreationMessage{
		Name: proto.String("Pick your top 2"),
		Options: []*waProto.PollCreationMessage_Option{
			{OptionName: proto.String("A")},
			{OptionName: proto.String("B")},
			{OptionName: proto.String("C")},
		},
		SelectableOptionsCount: proto.Uint32(2),
	}

	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chat,
				Sender:   sender,
				IsFromMe: false,
			},
			ID:        "poll-msg-2",
			Timestamp: time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC),
			PushName:  "Sender",
		},
		Message: &waProto.Message{PollCreationMessage: pollMsg},
	}

	pm := ParseLiveMessage(ev)

	if pm.Poll == nil {
		t.Fatalf("expected Poll to be set")
	}
	if pm.Poll.SelectableCount != 2 {
		t.Fatalf("expected SelectableCount 2, got %d", pm.Poll.SelectableCount)
	}
}

func TestParsePollVoteDetection(t *testing.T) {
	chat, _ := types.ParseJID("123@g.us")
	sender, _ := types.ParseJID("voter@s.whatsapp.net")

	// Poll vote messages have PollUpdateMessage
	voteMsg := &waProto.PollUpdateMessage{
		PollCreationMessageKey: &waProto.MessageKey{
			RemoteJID: proto.String("123@g.us"),
			FromMe:    proto.Bool(false),
			ID:        proto.String("poll-msg-1"),
		},
		// Vote is encrypted in EncPollVotePayload (not included in test proto)
	}

	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chat,
				Sender:   sender,
				IsFromMe: false,
			},
			ID:        "vote-msg-1",
			Timestamp: time.Date(2024, 3, 1, 12, 5, 0, 0, time.UTC),
			PushName:  "Voter",
		},
		Message: &waProto.Message{PollUpdateMessage: voteMsg},
	}

	pm := ParseLiveMessage(ev)

	// Verify vote detection
	if !pm.IsPollVote {
		t.Fatalf("expected IsPollVote to be true")
	}
	if pm.PollVoteRaw == nil {
		t.Fatalf("expected PollVoteRaw to be set")
	}
	if pm.Text != "[Vote]" {
		t.Fatalf("unexpected text for vote: %q", pm.Text)
	}
}

func TestParsePollCreationDisplayText(t *testing.T) {
	chat, _ := types.ParseJID("123@g.us")
	sender, _ := types.ParseJID("sender@s.whatsapp.net")

	tests := []struct {
		name         string
		pollQuestion string
		wantText     string
	}{
		{
			name:         "short question",
			pollQuestion: "Yes or No?",
			wantText:     "[Poll] Yes or No?",
		},
		{
			name:         "long question",
			pollQuestion: "What do you think about this very long poll question that contains a lot of text?",
			wantText:     "[Poll] What do you think about this very long poll question that contains a lot of text?",
		},
		{
			name:         "empty question",
			pollQuestion: "",
			wantText:     "[Poll] ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pollMsg := &waProto.PollCreationMessage{
				Name: proto.String(tt.pollQuestion),
				Options: []*waProto.PollCreationMessage_Option{
					{OptionName: proto.String("Option 1")},
				},
				SelectableOptionsCount: proto.Uint32(1),
			}

			ev := &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:     chat,
						Sender:   sender,
						IsFromMe: false,
					},
					ID:        "poll-msg",
					Timestamp: time.Now(),
					PushName:  "Sender",
				},
				Message: &waProto.Message{PollCreationMessage: pollMsg},
			}

			pm := ParseLiveMessage(ev)
			if pm.Text != tt.wantText {
				t.Fatalf("expected text %q, got %q", tt.wantText, pm.Text)
			}
		})
	}
}

func TestParsePollCreationFromHistory(t *testing.T) {
	// Test parsing poll from history sync message
	chatJID := "123@g.us"

	pollMsg := &waProto.PollCreationMessage{
		Name: proto.String("Historical poll"),
		Options: []*waProto.PollCreationMessage_Option{
			{OptionName: proto.String("Yes")},
			{OptionName: proto.String("No")},
		},
		SelectableOptionsCount: proto.Uint32(1),
	}

	histMsg := &waProto.WebMessageInfo{
		Key: &waProto.MessageKey{
			RemoteJID:   proto.String(chatJID),
			FromMe:      proto.Bool(false),
			ID:          proto.String("hist-poll-1"),
			Participant: proto.String("sender@s.whatsapp.net"),
		},
		MessageTimestamp: proto.Uint64(uint64(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix())),
		Message: &waProto.Message{
			PollCreationMessage: pollMsg,
		},
	}

	pm := ParseHistoryMessage(chatJID, histMsg)

	// Verify poll extraction from history
	if pm.Poll == nil {
		t.Fatalf("expected Poll to be set from history message")
	}
	if pm.Poll.Question != "Historical poll" {
		t.Fatalf("unexpected question: %q", pm.Poll.Question)
	}
	if len(pm.Poll.Options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(pm.Poll.Options))
	}
	if pm.Text != "[Poll] Historical poll" {
		t.Fatalf("unexpected text: %q", pm.Text)
	}
}

func TestParsePollWithNoOptions(t *testing.T) {
	// Edge case: poll with no options (shouldn't happen in practice)
	chat, _ := types.ParseJID("123@g.us")
	sender, _ := types.ParseJID("sender@s.whatsapp.net")

	pollMsg := &waProto.PollCreationMessage{
		Name:                   proto.String("Empty poll"),
		Options:                []*waProto.PollCreationMessage_Option{},
		SelectableOptionsCount: proto.Uint32(0),
	}

	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chat,
				Sender:   sender,
				IsFromMe: false,
			},
			ID:        "poll-msg-empty",
			Timestamp: time.Now(),
			PushName:  "Sender",
		},
		Message: &waProto.Message{PollCreationMessage: pollMsg},
	}

	pm := ParseLiveMessage(ev)

	if pm.Poll == nil {
		t.Fatalf("expected Poll to be set")
	}
	if len(pm.Poll.Options) != 0 {
		t.Fatalf("expected 0 options, got %d", len(pm.Poll.Options))
	}
	if pm.Poll.SelectableCount != 0 {
		t.Fatalf("expected SelectableCount 0, got %d", pm.Poll.SelectableCount)
	}
}
