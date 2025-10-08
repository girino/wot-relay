package newsqlite3

import (
	"context"
	"encoding/json"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
)

// Note: json import still needed for marshaling evt.Tags into event.tags column

func (b *SQLite3Backend) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	// Start a transaction
	tx, err := b.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert event
	tagsj, _ := json.Marshal(evt.Tags)
	res, err := tx.ExecContext(ctx, `
        INSERT OR IGNORE INTO event (id, pubkey, created_at, kind, tags, content, sig)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, evt.ID, evt.PubKey, evt.CreatedAt, evt.Kind, tagsj, evt.Content, evt.Sig)
	if err != nil {
		return err
	}

	nr, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if nr == 0 {
		return eventstore.ErrDupEvent
	}

	// Insert tags
	for i, tag := range evt.Tags {
		if len(tag) == 0 {
			continue // Skip empty tags
		}

		identifier := tag[0]
		var firstData *string
		if len(tag) > 1 {
			firstData = &tag[1]
		}

		_, err = tx.ExecContext(ctx, `
            INSERT INTO tag (event_id, tag_order, identifier, first_data)
            VALUES ($1, $2, $3, $4)
        `, evt.ID, i, identifier, firstData)
		if err != nil {
			return err
		}
	}

	// Commit transaction
	return tx.Commit()
}
