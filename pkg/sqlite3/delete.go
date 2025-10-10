package sqlite3

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

func (b *SQLite3Backend) DeleteEvent(ctx context.Context, evt *nostr.Event) error {
	// Start a transaction
	tx, err := b.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// First, delete all tags associated with this event
	_, err = tx.ExecContext(ctx, "DELETE FROM tag WHERE event_id = $1", evt.ID)
	if err != nil {
		return err
	}

	// Then delete the event itself
	_, err = tx.ExecContext(ctx, "DELETE FROM event WHERE id = $1", evt.ID)
	if err != nil {
		return err
	}

	// Commit transaction
	return tx.Commit()
}
