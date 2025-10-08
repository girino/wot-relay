package newsqlite3

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/nbd-wtf/go-nostr"
)

func (b *SQLite3Backend) QueryEvents(ctx context.Context, filter nostr.Filter) (ch chan *nostr.Event, err error) {
	query, params, err := b.queryEventsSql(filter, false)
	if err != nil {
		return nil, err
	}

	rows, err := b.DB.QueryContext(ctx, query, params...)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch events using query %q: %w", query, err)
	}

	ch = make(chan *nostr.Event)
	go func() {
		defer rows.Close()
		defer close(ch)
		for rows.Next() {
			var evt nostr.Event
			var timestamp int64
			err := rows.Scan(&evt.ID, &evt.PubKey, &timestamp,
				&evt.Kind, &evt.Tags, &evt.Content, &evt.Sig)
			if err != nil {
				return
			}
			evt.CreatedAt = nostr.Timestamp(timestamp)
			select {
			case ch <- &evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (b *SQLite3Backend) CountEvents(ctx context.Context, filter nostr.Filter) (int64, error) {
	query, params, err := b.queryEventsSql(filter, true)
	if err != nil {
		return 0, err
	}

	var count int64
	if err = b.DB.QueryRowContext(ctx, query, params...).Scan(&count); err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to fetch events using query %q: %w", query, err)
	}
	return count, nil
}

var (
	ErrTooManyIDs       = errors.New("too many ids")
	ErrTooManyAuthors   = errors.New("too many authors")
	ErrTooManyKinds     = errors.New("too many kinds")
	ErrTooManyTagValues = errors.New("too many tag values")
	ErrEmptyTagSet      = errors.New("empty tag set")
)

func makePlaceHolders(n int) string {
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func (b *SQLite3Backend) queryEventsSql(filter nostr.Filter, doCount bool) (string, []any, error) {
	conditions := make([]string, 0, 7)
	params := make([]any, 0, 20)

	if len(filter.IDs) > 0 {
		if len(filter.IDs) > 500 {
			// too many ids, fail everything
			return "", nil, ErrTooManyIDs
		}

		for _, v := range filter.IDs {
			params = append(params, v)
		}
		conditions = append(conditions, `id IN (`+makePlaceHolders(len(filter.IDs))+`)`)
	}

	if len(filter.Authors) > 0 {
		if len(filter.Authors) > b.QueryAuthorsLimit {
			// too many authors, fail everything
			return "", nil, ErrTooManyAuthors
		}

		for _, v := range filter.Authors {
			params = append(params, v)
		}
		conditions = append(conditions, `pubkey IN (`+makePlaceHolders(len(filter.Authors))+`)`)
	}

	if len(filter.Kinds) > 0 {
		if len(filter.Kinds) > b.QueryKindsLimit {
			// too many kinds, fail everything
			return "", nil, ErrTooManyKinds
		}

		for _, v := range filter.Kinds {
			params = append(params, v)
		}
		conditions = append(conditions, `kind IN (`+makePlaceHolders(len(filter.Kinds))+`)`)
	}

	// tags
	totalTags := 0
	// use the tag table for efficient tag filtering
	for tagKey, values := range filter.Tags {
		if len(values) == 0 {
			// any tag set to [] is wrong
			return "", nil, ErrEmptyTagSet
		}

		// For each tag key, we need to find events that have at least one matching tag value
		// We use a subquery with EXISTS to check if the event has a matching tag
		tagValuePlaceholders := makePlaceHolders(len(values))
		subquery := `EXISTS (
			SELECT 1 FROM tag 
			WHERE tag.event_id = event.id 
			AND tag.identifier = ?
			AND tag.first_data IN (` + tagValuePlaceholders + `)
		)`

		params = append(params, tagKey)
		for _, tagValue := range values {
			params = append(params, tagValue)
		}

		conditions = append(conditions, subquery)

		totalTags += len(values)
		if totalTags > b.QueryTagsLimit {
			// too many tags, fail everything
			return "", nil, ErrTooManyTagValues
		}
	}

	if filter.Since != nil {
		conditions = append(conditions, `created_at >= ?`)
		params = append(params, filter.Since)
	}
	if filter.Until != nil {
		conditions = append(conditions, `created_at <= ?`)
		params = append(params, filter.Until)
	}
	if filter.Search != "" {
		conditions = append(conditions, `content LIKE ? ESCAPE '\'`)
		params = append(params, `%`+strings.ReplaceAll(filter.Search, `%`, `\%`)+`%`)
	}

	if len(conditions) == 0 {
		// fallback
		conditions = append(conditions, `true`)
	}

	if filter.Limit < 1 || filter.Limit > b.QueryLimit {
		params = append(params, b.QueryLimit)
	} else {
		params = append(params, filter.Limit)
	}

	var query string
	if doCount {
		query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT
          COUNT(*)
        FROM event WHERE `+
			strings.Join(conditions, " AND ")+
			" LIMIT ?")
	} else {
		query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT
          id, pubkey, created_at, kind, tags, content, sig
        FROM event WHERE `+
			strings.Join(conditions, " AND ")+
			" ORDER BY created_at DESC, id LIMIT ?")
	}

	return query, params, nil
}
