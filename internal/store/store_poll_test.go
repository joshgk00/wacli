// +build sqlite_fts5

package store

import (
	"testing"
	"time"
)

func TestUpsertPoll(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	question := "What's your favorite color?"
	selectableCount := 1
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first (foreign key requirement)
	if err := db.UpsertChat(chatJID, "group", "Test Group", createdAt); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}

	err := db.UpsertPoll(chatJID, msgID, question, selectableCount, createdAt)
	if err != nil {
		t.Fatalf("UpsertPoll: %v", err)
	}

	// Verify it was stored
	n := countRows(t, db.sql, "SELECT COUNT(*) FROM polls WHERE chat_jid = ? AND msg_id = ?", chatJID, msgID)
	if n != 1 {
		t.Fatalf("expected 1 poll, got %d", n)
	}

	// Verify idempotency (upsert again should not create duplicate)
	err = db.UpsertPoll(chatJID, msgID, question, selectableCount, createdAt)
	if err != nil {
		t.Fatalf("UpsertPoll second time: %v", err)
	}
	n = countRows(t, db.sql, "SELECT COUNT(*) FROM polls WHERE chat_jid = ? AND msg_id = ?", chatJID, msgID)
	if n != 1 {
		t.Fatalf("expected 1 poll after upsert, got %d", n)
	}
}

func TestUpsertPollOption(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	if err := db.UpsertChat(chatJID, "group", "Test Group", createdAt); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}

	// Create poll first
	err := db.UpsertPoll(chatJID, msgID, "Question?", 1, createdAt)
	if err != nil {
		t.Fatalf("UpsertPoll: %v", err)
	}

	// Add options with hashes
	options := []struct {
		index int
		name  string
		hash  []byte
	}{
		{0, "Red", []byte{0x01, 0x02, 0x03}},
		{1, "Blue", []byte{0x04, 0x05, 0x06}},
		{2, "Green", []byte{0x07, 0x08, 0x09}},
	}

	for _, opt := range options {
		err := db.UpsertPollOption(chatJID, msgID, opt.index, opt.name, opt.hash)
		if err != nil {
			t.Fatalf("UpsertPollOption index %d: %v", opt.index, err)
		}
	}

	// Verify all stored
	n := countRows(t, db.sql, "SELECT COUNT(*) FROM poll_options WHERE chat_jid = ? AND msg_id = ?", chatJID, msgID)
	if n != 3 {
		t.Fatalf("expected 3 options, got %d", n)
	}

	// Test idempotency
	err = db.UpsertPollOption(chatJID, msgID, 0, "Red", []byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Fatalf("UpsertPollOption again: %v", err)
	}
	n = countRows(t, db.sql, "SELECT COUNT(*) FROM poll_options WHERE chat_jid = ? AND msg_id = ?", chatJID, msgID)
	if n != 3 {
		t.Fatalf("expected 3 options after upsert, got %d", n)
	}
}

func TestUpsertPollVote(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll and options
	_ = db.UpsertPoll(chatJID, msgID, "Question?", 1, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Option A", []byte{0x01})
	_ = db.UpsertPollOption(chatJID, msgID, 1, "Option B", []byte{0x02})

	voterJID := "voter1@s.whatsapp.net"
	votedAt := time.Date(2024, 3, 1, 12, 5, 0, 0, time.UTC)
	selectedIndices := []int{0}

	err := db.UpsertPollVote(chatJID, msgID, voterJID, selectedIndices, votedAt)
	if err != nil {
		t.Fatalf("UpsertPollVote: %v", err)
	}

	// Verify vote stored
	n := countRows(t, db.sql, "SELECT COUNT(*) FROM poll_votes WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?",
		chatJID, msgID, voterJID)
	if n != 1 {
		t.Fatalf("expected 1 vote, got %d", n)
	}

	// Verify selection stored
	n = countRows(t, db.sql, "SELECT COUNT(*) FROM poll_vote_selections WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?",
		chatJID, msgID, voterJID)
	if n != 1 {
		t.Fatalf("expected 1 selection, got %d", n)
	}
}

func TestUpsertPollVoteChange(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll with multi-select (max 2)
	_ = db.UpsertPoll(chatJID, msgID, "Question?", 2, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Option A", []byte{0x01})
	_ = db.UpsertPollOption(chatJID, msgID, 1, "Option B", []byte{0x02})
	_ = db.UpsertPollOption(chatJID, msgID, 2, "Option C", []byte{0x03})

	voterJID := "voter1@s.whatsapp.net"
	votedAt1 := time.Date(2024, 3, 1, 12, 5, 0, 0, time.UTC)
	votedAt2 := time.Date(2024, 3, 1, 12, 10, 0, 0, time.UTC)

	// First vote: select options 0 and 1
	err := db.UpsertPollVote(chatJID, msgID, voterJID, []int{0, 1}, votedAt1)
	if err != nil {
		t.Fatalf("UpsertPollVote first: %v", err)
	}

	n := countRows(t, db.sql, "SELECT COUNT(*) FROM poll_vote_selections WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?",
		chatJID, msgID, voterJID)
	if n != 2 {
		t.Fatalf("expected 2 selections after first vote, got %d", n)
	}

	// Change vote: now select only option 2
	err = db.UpsertPollVote(chatJID, msgID, voterJID, []int{2}, votedAt2)
	if err != nil {
		t.Fatalf("UpsertPollVote change: %v", err)
	}

	// Old selections should be replaced
	n = countRows(t, db.sql, "SELECT COUNT(*) FROM poll_vote_selections WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?",
		chatJID, msgID, voterJID)
	if n != 1 {
		t.Fatalf("expected 1 selection after vote change, got %d", n)
	}

	// Verify the new selection is option 2
	row := db.sql.QueryRow("SELECT option_index FROM poll_vote_selections WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?",
		chatJID, msgID, voterJID)
	var idx int
	if err := row.Scan(&idx); err != nil {
		t.Fatalf("scan option_index: %v", err)
	}
	if idx != 2 {
		t.Fatalf("expected option_index 2, got %d", idx)
	}
}

func TestLookupOptionByHash(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll with options
	_ = db.UpsertPoll(chatJID, msgID, "Question?", 1, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Red", []byte{0xAA, 0xBB})
	_ = db.UpsertPollOption(chatJID, msgID, 1, "Blue", []byte{0xCC, 0xDD})
	_ = db.UpsertPollOption(chatJID, msgID, 2, "Green", []byte{0xEE, 0xFF})

	// Lookup by hash
	idx, name, err := db.LookupOptionByHash(chatJID, msgID, []byte{0xCC, 0xDD})
	if err != nil {
		t.Fatalf("LookupOptionByHash: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}
	if name != "Blue" {
		t.Fatalf("expected name Blue, got %q", name)
	}
}

func TestLookupOptionByHashNotFound(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll with options
	_ = db.UpsertPoll(chatJID, msgID, "Question?", 1, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Red", []byte{0xAA, 0xBB})

	// Lookup non-existent hash
	_, _, err := db.LookupOptionByHash(chatJID, msgID, []byte{0xFF, 0xFF})
	if err == nil {
		t.Fatalf("expected error for non-existent hash")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound error, got %v", err)
	}
}

func TestGetPollResults(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll
	_ = db.UpsertPoll(chatJID, msgID, "What's your favorite color?", 1, createdAt)

	// Add options
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Red", []byte{0x01})
	_ = db.UpsertPollOption(chatJID, msgID, 1, "Blue", []byte{0x02})
	_ = db.UpsertPollOption(chatJID, msgID, 2, "Green", []byte{0x03})

	// Add votes
	_ = db.UpsertPollVote(chatJID, msgID, "voter1@s.whatsapp.net", []int{0}, createdAt.Add(1*time.Minute))
	_ = db.UpsertPollVote(chatJID, msgID, "voter2@s.whatsapp.net", []int{0}, createdAt.Add(2*time.Minute))
	_ = db.UpsertPollVote(chatJID, msgID, "voter3@s.whatsapp.net", []int{1}, createdAt.Add(3*time.Minute))

	// Get results
	results, err := db.GetPollResults(chatJID, msgID)
	if err != nil {
		t.Fatalf("GetPollResults: %v", err)
	}

	// Verify poll metadata
	if results.Question != "What's your favorite color?" {
		t.Fatalf("unexpected question: %q", results.Question)
	}
	if results.SelectableCount != 1 {
		t.Fatalf("unexpected selectable count: %d", results.SelectableCount)
	}
	if !results.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected created_at: %s", results.CreatedAt)
	}

	// Verify options
	if len(results.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(results.Options))
	}
	if results.Options[0].Name != "Red" || results.Options[0].Index != 0 {
		t.Fatalf("unexpected option 0: %+v", results.Options[0])
	}

	// Verify vote counts
	if results.Options[0].VoteCount != 2 {
		t.Fatalf("expected option 0 to have 2 votes, got %d", results.Options[0].VoteCount)
	}
	if results.Options[1].VoteCount != 1 {
		t.Fatalf("expected option 1 to have 1 vote, got %d", results.Options[1].VoteCount)
	}
	if results.Options[2].VoteCount != 0 {
		t.Fatalf("expected option 2 to have 0 votes, got %d", results.Options[2].VoteCount)
	}

	// Verify votes
	if len(results.Votes) != 3 {
		t.Fatalf("expected 3 votes, got %d", len(results.Votes))
	}

	// Votes should be sorted by timestamp DESC (most recent first)
	if results.Votes[0].VoterJID != "voter3@s.whatsapp.net" {
		t.Fatalf("expected first vote from voter3, got %q", results.Votes[0].VoterJID)
	}

	// Verify selected indices
	if len(results.Votes[0].SelectedIndices) != 1 || results.Votes[0].SelectedIndices[0] != 1 {
		t.Fatalf("unexpected selections for voter3: %v", results.Votes[0].SelectedIndices)
	}
}

func TestGetPollResultsEmpty(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create poll with no votes
	_ = db.UpsertPoll(chatJID, msgID, "No votes yet", 1, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Option A", []byte{0x01})

	results, err := db.GetPollResults(chatJID, msgID)
	if err != nil {
		t.Fatalf("GetPollResults: %v", err)
	}

	if len(results.Options) != 1 {
		t.Fatalf("expected 1 option, got %d", len(results.Options))
	}
	if results.Options[0].VoteCount != 0 {
		t.Fatalf("expected 0 votes, got %d", results.Options[0].VoteCount)
	}
	if len(results.Votes) != 0 {
		t.Fatalf("expected 0 votes, got %d", len(results.Votes))
	}
}

func TestGetPollResultsNotFound(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "non-existent"

	_, err := db.GetPollResults(chatJID, msgID)
	if err == nil {
		t.Fatalf("expected error for non-existent poll")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound error, got %v", err)
	}
}

func TestGetPollResultsMultiSelect(t *testing.T) {
	db := openTestDB(t)

	chatJID := "123@g.us"
	msgID := "poll-msg-1"
	createdAt := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	// Create chat first
	_ = db.UpsertChat(chatJID, "group", "Test Group", createdAt)

	// Create multi-select poll (max 2)
	_ = db.UpsertPoll(chatJID, msgID, "Pick your top 2 colors", 2, createdAt)
	_ = db.UpsertPollOption(chatJID, msgID, 0, "Red", []byte{0x01})
	_ = db.UpsertPollOption(chatJID, msgID, 1, "Blue", []byte{0x02})
	_ = db.UpsertPollOption(chatJID, msgID, 2, "Green", []byte{0x03})

	// Voter selects 2 options
	_ = db.UpsertPollVote(chatJID, msgID, "voter1@s.whatsapp.net", []int{0, 2}, createdAt.Add(1*time.Minute))

	results, err := db.GetPollResults(chatJID, msgID)
	if err != nil {
		t.Fatalf("GetPollResults: %v", err)
	}

	// Vote count should reflect multi-select (each selected option gets +1)
	if results.Options[0].VoteCount != 1 {
		t.Fatalf("expected option 0 to have 1 vote, got %d", results.Options[0].VoteCount)
	}
	if results.Options[1].VoteCount != 0 {
		t.Fatalf("expected option 1 to have 0 votes, got %d", results.Options[1].VoteCount)
	}
	if results.Options[2].VoteCount != 1 {
		t.Fatalf("expected option 2 to have 1 vote, got %d", results.Options[2].VoteCount)
	}

	// Total voters should be 1 (not 2)
	if len(results.Votes) != 1 {
		t.Fatalf("expected 1 voter, got %d", len(results.Votes))
	}

	// Voter should have 2 selections
	if len(results.Votes[0].SelectedIndices) != 2 {
		t.Fatalf("expected 2 selections, got %d", len(results.Votes[0].SelectedIndices))
	}
}
