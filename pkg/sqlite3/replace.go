package sqlite3

import (
	"context"
	"fmt"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
)

// isOlder checks if the previous event is older than the next event.
// An event is considered older if it has an earlier created_at timestamp,
// or if the timestamps are equal, if it has a lexicographically greater ID.
func isOlder(previous, next *nostr.Event) bool {
	return previous.CreatedAt < next.CreatedAt ||
		(previous.CreatedAt == next.CreatedAt && previous.ID > next.ID)
}

func (b *SQLite3Backend) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	b.Lock()
	defer b.Unlock()

	filter := nostr.Filter{Limit: 1, Kinds: []int{evt.Kind}, Authors: []string{evt.PubKey}}
	if nostr.IsAddressableKind(evt.Kind) {
		filter.Tags = nostr.TagMap{"d": []string{evt.Tags.GetD()}}
	}

	ch, err := b.QueryEvents(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to query before replacing: %w", err)
	}

	shouldStore := true
	for previous := range ch {
		if isOlder(previous, evt) {
			if err := b.DeleteEvent(ctx, previous); err != nil {
				return fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := b.SaveEvent(ctx, evt); err != nil && err != eventstore.ErrDupEvent {
			return fmt.Errorf("failed to save: %w", err)
		}
	}

	return nil
}
