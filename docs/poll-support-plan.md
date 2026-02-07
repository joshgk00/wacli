# Poll Support Implementation Plan

## Overview
This document outlines the implementation plan for adding poll results support to wacli. Poll creation is already implemented via `wacli send poll`. This plan covers:
- Storing poll metadata and votes
- Capturing poll vote events during sync
- Displaying poll results via CLI
- Vote decryption and option hash matching

## Architecture Review

### Current State
- **Poll Creation**: `wacli send poll` works (sends polls via `BuildPollCreation`)
- **Message Storage**: Messages are stored in `messages` table with FTS5 indexing
- **Sync Flow**: Events are captured via `events.Message` and `events.HistorySync`
- **Database**: SQLite with WAL mode, foreign key constraints enabled
- **Pattern**: Cobra CLI commands + WAClient interface + store layer

### whatsmeow Poll API
1. **BuildPollCreation(name string, optionNames []string, selectableOptionCount int)** → Creates poll
2. **DecryptPollVote(ctx, *events.Message)** → Returns `*waE2E.PollVoteMessage` with selected option hashes
3. **BuildPollVote(ctx, pollInfo, optionNames)** → Creates vote message (for voting - future)
4. **HashPollOptions(optionNames []string)** → Returns SHA-256 hashes of option names (in `go.mau.fi/whatsmeow` package)

### Vote Flow
1. Poll creation message arrives → store poll metadata (question, options, option hashes)
2. Vote message arrives (encrypted) → decrypt via `DecryptPollVote()`
3. Vote contains hashes of selected options → match to original option names
4. Store vote (voter JID, poll msg ID, selected options, timestamp)

---

## Phase 1: Database Schema Changes

### New Tables

#### `polls` table
Stores poll metadata extracted from poll creation messages.

```sql
CREATE TABLE IF NOT EXISTS polls (
    chat_jid TEXT NOT NULL,
    msg_id TEXT NOT NULL,
    question TEXT NOT NULL,
    selectable_count INTEGER NOT NULL,  -- max options voter can select
    created_at INTEGER NOT NULL,
    PRIMARY KEY (chat_jid, msg_id),
    FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_polls_chat ON polls(chat_jid);
```

#### `poll_options` table
Stores poll options with their SHA-256 hashes for matching votes.

```sql
CREATE TABLE IF NOT EXISTS poll_options (
    chat_jid TEXT NOT NULL,
    msg_id TEXT NOT NULL,
    option_index INTEGER NOT NULL,       -- 0-based index from poll creation
    option_name TEXT NOT NULL,
    option_hash BLOB NOT NULL,           -- SHA-256 hash for vote matching
    PRIMARY KEY (chat_jid, msg_id, option_index),
    FOREIGN KEY (chat_jid, msg_id) REFERENCES polls(chat_jid, msg_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_poll_options_hash ON poll_options(chat_jid, msg_id, option_hash);
```

#### `poll_votes` table
Stores individual votes (one row per voter).

```sql
CREATE TABLE IF NOT EXISTS poll_votes (
    chat_jid TEXT NOT NULL,
    msg_id TEXT NOT NULL,
    voter_jid TEXT NOT NULL,
    voted_at INTEGER NOT NULL,
    PRIMARY KEY (chat_jid, msg_id, voter_jid),
    FOREIGN KEY (chat_jid, msg_id) REFERENCES polls(chat_jid, msg_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_poll_votes_poll ON poll_votes(chat_jid, msg_id);
```

#### `poll_vote_selections` table
Stores the actual selected options for each vote (handles multi-select).

```sql
CREATE TABLE IF NOT EXISTS poll_vote_selections (
    chat_jid TEXT NOT NULL,
    msg_id TEXT NOT NULL,
    voter_jid TEXT NOT NULL,
    option_index INTEGER NOT NULL,
    PRIMARY KEY (chat_jid, msg_id, voter_jid, option_index),
    FOREIGN KEY (chat_jid, msg_id, voter_jid) REFERENCES poll_votes(chat_jid, msg_id, voter_jid) ON DELETE CASCADE,
    FOREIGN KEY (chat_jid, msg_id, option_index) REFERENCES poll_options(chat_jid, msg_id, option_index) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_poll_vote_selections_poll ON poll_vote_selections(chat_jid, msg_id);
```

### Store Methods (internal/store/store.go)

Add the following methods to `DB`:

```go
// UpsertPoll stores poll metadata
func (d *DB) UpsertPoll(chatJID, msgID, question string, selectableCount int, createdAt time.Time) error

// UpsertPollOption stores a poll option with its hash
func (d *DB) UpsertPollOption(chatJID, msgID string, index int, name string, hash []byte) error

// UpsertPollVote stores a vote (upserts to handle vote changes)
func (d *DB) UpsertPollVote(chatJID, msgID, voterJID string, optionIndices []int, votedAt time.Time) error

// GetPollResults retrieves poll metadata, options, and vote counts
func (d *DB) GetPollResults(chatJID, msgID string) (PollResults, error)

// LookupOptionByHash finds option index by hash (for decrypting votes)
func (d *DB) LookupOptionByHash(chatJID, msgID string, hash []byte) (int, string, error)
```

### Domain Types (internal/store/store.go)

```go
type PollResults struct {
    ChatJID         string
    MsgID           string
    Question        string
    SelectableCount int
    CreatedAt       time.Time
    Options         []PollOption
    Votes           []PollVote
}

type PollOption struct {
    Index      int
    Name       string
    Hash       []byte  // SHA-256
    VoteCount  int     // computed from votes
}

type PollVote struct {
    VoterJID       string
    VoterName      string  // resolved display name
    SelectedIndices []int
    VotedAt        time.Time
}
```

---

## Phase 2: Message Parser Changes

### Extract Poll Creation (internal/wa/messages.go)

Add poll extraction to `extractWAProto()`:

```go
// Add to ParsedMessage struct:
type ParsedMessage struct {
    // ... existing fields ...
    Poll *PollCreation
}

type PollCreation struct {
    Question        string
    Options         []string
    SelectableCount int
}

// In extractWAProto():
if pollCreate := m.GetPollCreationMessage(); pollCreate != nil {
    options := make([]string, len(pollCreate.GetOptions()))
    for i, opt := range pollCreate.GetOptions() {
        options[i] = opt.GetOptionName()
    }
    pm.Poll = &PollCreation{
        Question:        pollCreate.GetName(),
        Options:         options,
        SelectableCount: int(pollCreate.GetSelectableOptionsCount()),
    }
    // Set display text
    if pm.Text == "" {
        pm.Text = fmt.Sprintf("[Poll] %s", pollCreate.GetName())
    }
}
```

### Detect Poll Vote Messages

Poll votes arrive as `PollUpdateMessage` (encrypted). They need to be flagged for decryption.

Add to `ParsedMessage`:

```go
type ParsedMessage struct {
    // ... existing fields ...
    IsPollVote     bool
    PollVoteRaw    *events.Message  // raw event for decryption
}
```

Detect in `ParseLiveMessage()`:

```go
func ParseLiveMessage(evt *events.Message) ParsedMessage {
    msg := ParsedMessage{
        // ... existing initialization ...
    }
    
    if evt.Message != nil && evt.Message.GetPollUpdateMessage() != nil {
        msg.IsPollVote = true
        msg.PollVoteRaw = evt  // store for later decryption
        msg.Text = "[Vote]"    // placeholder
    }
    
    extractWAProto(evt.Message, &msg)
    return msg
}
```

---

## Phase 3: Sync Handler Changes

### Poll Creation Handling (internal/app/sync.go)

In `storeParsedMessage()`, add poll creation handling:

```go
// After upserting message, check for poll creation
if pm.Poll != nil {
    if err := a.db.UpsertPoll(chatJID, pm.ID, pm.Poll.Question, pm.Poll.SelectableCount, pm.Timestamp); err != nil {
        // Log error but continue (best-effort)
    }
    
    // Store options with hashes
    for i, optName := range pm.Poll.Options {
        hash := hashPollOption(optName)  // SHA-256 helper
        if err := a.db.UpsertPollOption(chatJID, pm.ID, i, optName, hash); err != nil {
            // Log error but continue
        }
    }
}
```

### Poll Vote Handling (internal/app/sync.go)

In the event handler (where we already handle reactions):

```go
case *events.Message:
    pm := wa.ParseLiveMessage(v)
    
    // Handle poll votes (after storing the vote message itself)
    if pm.IsPollVote && pm.PollVoteRaw != nil {
        if err := a.handlePollVote(ctx, pm); err != nil {
            // Log error but don't fail sync
        }
    }
    
    // ... existing reaction handling ...
    
    if err := a.storeParsedMessage(ctx, pm); err == nil {
        messagesStored.Add(1)
    }
```

### New Handler Method (internal/app/sync.go)

```go
func (a *App) handlePollVote(ctx context.Context, pm wa.ParsedMessage) error {
    // Decrypt the vote
    voteMsg, err := a.wa.DecryptPollVote(ctx, pm.PollVoteRaw)
    if err != nil {
        return fmt.Errorf("decrypt poll vote: %w", err)
    }
    
    // Extract poll reference from the vote message
    pollRef := pm.PollVoteRaw.Message.GetPollUpdateMessage().GetPollCreationMessageKey()
    if pollRef == nil {
        return fmt.Errorf("no poll reference in vote")
    }
    
    pollMsgID := pollRef.GetID()
    chatJID := pm.Chat.String()
    voterJID := pm.SenderJID
    if pm.FromMe {
        // Use our own JID if voting ourselves
        if a.wa.IsAuthed() {
            // Get our JID from whatsmeow client store
            voterJID = "me"  // or resolve actual JID
        }
    }
    
    // Match vote hashes to option indices
    selectedHashes := voteMsg.GetSelectedOptions()
    selectedIndices := []int{}
    
    for _, hash := range selectedHashes {
        idx, _, err := a.db.LookupOptionByHash(chatJID, pollMsgID, hash)
        if err != nil {
            // Log warning: vote for unknown option (shouldn't happen)
            continue
        }
        selectedIndices = append(selectedIndices, idx)
    }
    
    // Store the vote
    return a.db.UpsertPollVote(chatJID, pollMsgID, voterJID, selectedIndices, pm.Timestamp)
}
```

### Helper Function (internal/app/sync.go or internal/wa/messages.go)

```go
func hashPollOption(optionName string) []byte {
    hash := sha256.Sum256([]byte(optionName))
    return hash[:]
}
```

---

## Phase 4: WAClient Interface Updates

### Add to WAClient Interface (internal/app/app.go)

```go
type WAClient interface {
    // ... existing methods ...
    
    DecryptPollVote(ctx context.Context, vote *events.Message) (*waProto.PollVoteMessage, error)
    // Note: DecryptReaction is already there, same pattern
}
```

The `wa.Client` already implements `DecryptPollVote`, so no changes needed in `internal/wa/client.go`.

### Update fake_wa_test.go

Add mock implementation:

```go
func (f *fakeWA) DecryptPollVote(ctx context.Context, vote *events.Message) (*waProto.PollVoteMessage, error) {
    return nil, fmt.Errorf("not implemented in fake")
}
```

---

## Phase 5: CLI Commands

### New Command: `wacli poll` (cmd/wacli/poll.go)

Create new file `cmd/wacli/poll.go`:

```go
package main

import (
    "github.com/spf13/cobra"
)

func newPollCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "poll",
        Short: "Poll operations",
    }
    cmd.AddCommand(newPollResultsCmd(flags))
    return cmd
}
```

### Subcommand: `wacli poll results` (cmd/wacli/poll.go)

```go
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
            
            a, lk, err := newApp(ctx, flags, true, false)
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
            
            return displayPollResults(results, flags.asJSON)
        },
    }
    
    cmd.Flags().StringVar(&chatJID, "chat", "", "Chat JID where poll was sent")
    cmd.Flags().StringVar(&msgID, "id", "", "Poll message ID")
    cmd.MarkFlagRequired("chat")
    cmd.MarkFlagRequired("id")
    
    return cmd
}
```

### Display Helper (cmd/wacli/poll.go)

```go
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
    if total == 0 {
        return strings.Repeat("░", width)
    }
    filled := int(float64(count) / float64(total) * float64(width))
    if filled > width {
        filled = width
    }
    return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
```

### Register Command (cmd/wacli/main.go)

In `main.go`, add poll command:

```go
rootCmd.AddCommand(newPollCmd(flags))
```

---

## Phase 6: Edge Cases & Robustness

### 1. Votes from History Sync

**Issue**: Historical poll votes arrive via `events.HistorySync`, not live `events.Message`.

**Solution**: Parse history sync messages for poll updates:

```go
// In sync.go, history sync handler:
for _, m := range conv.Messages {
    if m.Message == nil {
        continue
    }
    pm := wa.ParseHistoryMessage(chatID, m.Message)
    
    // Check for poll creation in history
    if pm.Poll != nil {
        // Store poll metadata (same as live)
    }
    
    // Check for poll votes in history
    if m.Message.Message.GetPollUpdateMessage() != nil {
        // Create fake events.Message for DecryptPollVote
        fakeEvt := &events.Message{
            Info: types.MessageInfo{
                Chat:      pm.Chat,
                Sender:    types.JID{}, // parse from m.Message
                Timestamp: pm.Timestamp,
                ID:        pm.ID,
                IsFromMe:  pm.FromMe,
            },
            Message: m.Message.Message,
        }
        // Decrypt and store vote
        _ = a.handlePollVote(ctx, wa.ParsedMessage{
            IsPollVote:  true,
            PollVoteRaw: fakeEvt,
            // ... other fields
        })
    }
    
    if err := a.storeParsedMessage(ctx, pm); err == nil {
        messagesStored.Add(1)
    }
}
```

### 2. Late Votes

**Issue**: A vote arrives before the poll creation message is synced.

**Solution**: 
- Store vote even if poll doesn't exist yet (foreign key constraint will fail)
- Use `ON CONFLICT IGNORE` or check existence first
- During `GetPollResults()`, handle missing options gracefully

**Implementation**: Make foreign key constraints **DEFERRABLE** or remove them, relying on application logic:

```sql
-- Remove FOREIGN KEY constraints on poll_votes and poll_vote_selections
-- Or use: FOREIGN KEY ... DEFERRABLE INITIALLY DEFERRED
```

Better approach: Check poll existence before storing vote:

```go
func (d *DB) UpsertPollVote(chatJID, msgID, voterJID string, optionIndices []int, votedAt time.Time) error {
    // Check if poll exists
    exists, err := d.pollExists(chatJID, msgID)
    if err != nil {
        return err
    }
    if !exists {
        // Log warning: vote for non-existent poll (will store when poll arrives)
        return fmt.Errorf("poll %s/%s not found (late vote)", chatJID, msgID)
    }
    
    // Store vote ...
}
```

### 3. Vote Changes

**Issue**: Users can change their vote. WhatsApp sends a new vote message.

**Solution**: `UpsertPollVote` uses `ON CONFLICT ... DO UPDATE`:

```go
// First, delete old selections
_, err := tx.Exec(`DELETE FROM poll_vote_selections WHERE chat_jid = ? AND msg_id = ? AND voter_jid = ?`,
    chatJID, msgID, voterJID)

// Then insert new vote
_, err = tx.Exec(`INSERT INTO poll_votes (chat_jid, msg_id, voter_jid, voted_at) 
    VALUES (?, ?, ?, ?)
    ON CONFLICT (chat_jid, msg_id, voter_jid) DO UPDATE SET voted_at = excluded.voted_at`,
    chatJID, msgID, voterJID, unix(votedAt))
```

### 4. Polls Sent by Us

**Issue**: When we send a poll, we need to store it immediately (not wait for echo).

**Solution**: Already handled in `send.go` via `UpsertMessage`. Extend to store poll:

```go
// In newSendPollCmd:
msgID, err := a.WA().SendPoll(ctx, toJID, question, options, maxSelectable)

// Store poll metadata
_ = a.DB().UpsertPoll(chat.String(), string(msgID), question, maxSelectable, now)
for i, opt := range options {
    hash := hashPollOption(opt)
    _ = a.DB().UpsertPollOption(chat.String(), string(msgID), i, opt, hash)
}
```

### 5. Missing Poll Metadata

**Issue**: `GetPollResults()` called for a poll we never saw.

**Solution**: Return `store.ErrNotFound` (already using `sql.ErrNoRows`).

```go
func (d *DB) GetPollResults(chatJID, msgID string) (PollResults, error) {
    // Query polls table
    row := d.sql.QueryRow(`SELECT question, selectable_count, created_at FROM polls WHERE chat_jid = ? AND msg_id = ?`, chatJID, msgID)
    
    var results PollResults
    var createdAt int64
    if err := row.Scan(&results.Question, &results.SelectableCount, &createdAt); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return PollResults{}, err  // IsNotFound will detect this
        }
        return PollResults{}, err
    }
    results.CreatedAt = fromUnix(createdAt)
    
    // Load options and votes ...
    return results, nil
}
```

### 6. Vote Decryption Failures

**Issue**: `DecryptPollVote()` can fail (corrupted message, missing keys).

**Solution**: Log error, skip vote, continue sync:

```go
if err := a.handlePollVote(ctx, pm); err != nil {
    fmt.Fprintf(os.Stderr, "Warning: failed to decrypt poll vote: %v\n", err)
    // Don't return error, continue processing
}
```

---

## Phase 7: Testing Strategy

### Unit Tests

1. **store_test.go**: Test poll CRUD operations
   - Create poll with options
   - Store votes
   - Update votes (vote changes)
   - Retrieve results with counts
   - Lookup option by hash

2. **messages_test.go**: Test poll message parsing
   - Parse poll creation from protobuf
   - Detect poll vote messages

### Integration Tests

1. **Manual testing with real WhatsApp**:
   - Send poll, vote, check results
   - Multi-select poll
   - Vote changes
   - Sync historical polls

2. **Test with history sync**:
   - Fresh auth, sync old polls
   - Verify votes from history are stored

### Build & Deploy Test

```bash
cd /tmp/wacli-poll
go build -tags sqlite_fts5 -o ./dist/wacli ./cmd/wacli
./dist/wacli poll --help
./dist/wacli poll results --help
cp ./dist/wacli /Users/slaughterassistant/.local/bin/wacli-poll
```

---

## Implementation Order

### Step 1: Database Schema (Day 1)
- [ ] Add poll tables to `ensureSchema()`
- [ ] Add domain types (`PollResults`, etc.)
- [ ] Implement store methods (`UpsertPoll`, `GetPollResults`, etc.)
- [ ] Write unit tests for store

### Step 2: Message Parsing (Day 1)
- [ ] Extend `ParsedMessage` with poll fields
- [ ] Extract poll creation in `extractWAProto()`
- [ ] Detect poll votes in `ParseLiveMessage()`
- [ ] Write tests for message parsing

### Step 3: Sync Handlers (Day 2)
- [ ] Handle poll creation in `storeParsedMessage()`
- [ ] Implement `handlePollVote()` with decryption
- [ ] Handle history sync polls/votes
- [ ] Handle polls sent by us (extend send command)
- [ ] Test with live WhatsApp connection

### Step 4: CLI Commands (Day 2)
- [ ] Create `cmd/wacli/poll.go`
- [ ] Implement `poll results` command
- [ ] Implement human-readable output with bar charts
- [ ] Implement JSON output
- [ ] Register command in `main.go`
- [ ] Test CLI output

### Step 5: Edge Cases (Day 3)
- [ ] Handle late votes gracefully
- [ ] Handle vote changes (upsert logic)
- [ ] Handle missing poll metadata
- [ ] Handle decryption failures
- [ ] Test all edge cases

### Step 6: Build & Final Testing (Day 3)
- [ ] Build with FTS5 tags
- [ ] Copy to local bin
- [ ] End-to-end test with real WhatsApp
- [ ] Document usage in README

---

## Success Criteria

- [ ] `wacli poll results --chat JID --id MSG_ID` displays poll results
- [ ] Vote counts are accurate
- [ ] Voter names are resolved
- [ ] Historical polls/votes are captured during sync
- [ ] Vote changes update correctly
- [ ] JSON output works for scripting
- [ ] No crashes on missing data
- [ ] Works with both DMs and groups

---

## Future Enhancements (Out of Scope for v1)

- `wacli poll vote` - Vote on a poll via CLI
- `wacli poll list` - List all polls in a chat
- Interactive poll result updates (live view)
- Export poll results to CSV
- Poll analytics (time-series of votes)

---

## Notes

- **Vote privacy**: WhatsApp encrypts votes end-to-end, but once decrypted, wacli stores them locally. This is expected for a local CLI tool.
- **SHA-256 matching**: The `HashPollOptions` function is in the whatsmeow package, not a method. Import and use directly.
- **Foreign keys**: Consider making constraints deferrable or using application-level checks for late votes.
- **Display names**: Resolve voter names via contact store (best effort).

---

## References

- whatsmeow docs: https://pkg.go.dev/go.mau.fi/whatsmeow
- wacli spec: `/tmp/wacli-poll/docs/spec.md`
- Existing patterns: `send.go`, `sync.go`, `store.go`
