package newsqlite3

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/girino/wot-relay/pkg/logger"
	"github.com/jmoiron/sqlx"
	"github.com/nbd-wtf/go-nostr"
)

func (b *SQLite3Backend) QueryEvents(ctx context.Context, filter nostr.Filter) (ch chan *nostr.Event, err error) {
	query, params, err := b.queryEventsSql(filter, false)
	if err != nil {
		return nil, err
	}

	// Start timing DB execution
	start := time.Now()
	rows, err := b.DB.QueryContext(ctx, query, params...)
	duration := time.Since(start)

	// Log slow queries
	if b.SlowQueryThreshold > 0 && duration > b.SlowQueryThreshold {
		logger.Warn("SLOW_QUERY", "QueryEvents slow execution", map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
			"query":       query,
			"params":      params,
		})
	}

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch events using query %q: %w", query, err)
	}

	// Use buffered channel to reduce blocking (buffer size = query limit)
	bufferSize := filter.Limit
	if bufferSize < 1 || bufferSize > b.QueryLimit {
		bufferSize = b.QueryLimit
	}
	ch = make(chan *nostr.Event, bufferSize)

	go func() {
		defer rows.Close()
		defer close(ch)

		// Track row processing time
		rowStart := time.Now()
		rowCount := 0
		scanTime := time.Duration(0)
		sendTime := time.Duration(0)

		for rows.Next() {
			scanStart := time.Now()
			var evt nostr.Event
			var timestamp int64

			err := rows.Scan(&evt.ID, &evt.PubKey, &timestamp,
				&evt.Kind, &evt.Tags, &evt.Content, &evt.Sig)
			if err != nil {
				return
			}
			evt.CreatedAt = nostr.Timestamp(timestamp)
			scanTime += time.Since(scanStart)

			rowCount++
			sendStart := time.Now()
			select {
			case ch <- &evt:
				sendTime += time.Since(sendStart)
			case <-ctx.Done():
				return
			}
		}

		// Measure total row processing time
		rowDuration := time.Since(rowStart)

		// Log slow row processing with detailed breakdown
		if b.SlowQueryThreshold > 0 && rowDuration > b.SlowQueryThreshold {
			logger.Warn("SLOW_ROW_PROCESSING", "QueryEvents slow row scan", map[string]interface{}{
				"duration_ms": rowDuration.Milliseconds(),
				"rows":        rowCount,
				"scan_ms":     scanTime.Milliseconds(),
				"send_ms":     sendTime.Milliseconds(),
				"query":       query,
				"params":      params,
			})
		}
	}()

	return ch, nil
}

func (b *SQLite3Backend) CountEvents(ctx context.Context, filter nostr.Filter) (int64, error) {
	query, params, err := b.queryEventsSql(filter, true)
	if err != nil {
		return 0, err
	}

	// Start timing
	start := time.Now()

	var count int64
	err = b.DB.QueryRowContext(ctx, query, params...).Scan(&count)

	// Measure query execution time
	duration := time.Since(start)

	// Log slow queries
	if b.SlowQueryThreshold > 0 && duration > b.SlowQueryThreshold {
		logger.Warn("SLOW_QUERY", "CountEvents slow execution", map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
			"query":       query,
			"params":      params,
			"count":       count,
		})
	}

	if err != nil && err != sql.ErrNoRows {
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
	whereParams := make([]any, 0, 20) // Params for WHERE clause
	joinParams := make([]any, 0, 10)  // Params for JOIN clause
	joins := make([]string, 0, len(filter.Tags))
	tagIndex := 0

	if len(filter.IDs) > 0 {
		if len(filter.IDs) > 500 {
			// too many ids, fail everything
			return "", nil, ErrTooManyIDs
		}

		for _, v := range filter.IDs {
			whereParams = append(whereParams, v)
		}
		conditions = append(conditions, `event.id IN (`+makePlaceHolders(len(filter.IDs))+`)`)
	}

	if len(filter.Authors) > 0 {
		if len(filter.Authors) > b.QueryAuthorsLimit {
			// too many authors, fail everything
			return "", nil, ErrTooManyAuthors
		}

		for _, v := range filter.Authors {
			whereParams = append(whereParams, v)
		}
		conditions = append(conditions, `event.pubkey IN (`+makePlaceHolders(len(filter.Authors))+`)`)
	}

	if len(filter.Kinds) > 0 {
		if len(filter.Kinds) > b.QueryKindsLimit {
			// too many kinds, fail everything
			return "", nil, ErrTooManyKinds
		}

		for _, v := range filter.Kinds {
			whereParams = append(whereParams, v)
		}
		conditions = append(conditions, `event.kind IN (`+makePlaceHolders(len(filter.Kinds))+`)`)
	}

	// tags - use JOIN for better performance
	// Tag params go in joinParams since they appear in JOIN clause (before WHERE)
	totalTags := 0
	for tagKey, values := range filter.Tags {
		if len(values) == 0 {
			// any tag set to [] is wrong
			return "", nil, ErrEmptyTagSet
		}

		// Use INNER JOIN instead of EXISTS for much better performance
		// Each tag filter gets its own join with an alias
		tagAlias := fmt.Sprintf("tag%d", tagIndex)
		tagValuePlaceholders := makePlaceHolders(len(values))

		joins = append(joins, fmt.Sprintf(`INNER JOIN tag AS %s ON %s.event_id = event.id 
			AND %s.identifier = ? 
			AND %s.first_data IN (%s)`,
			tagAlias, tagAlias, tagAlias, tagAlias, tagValuePlaceholders))

		joinParams = append(joinParams, tagKey)
		for _, tagValue := range values {
			joinParams = append(joinParams, tagValue)
		}

		tagIndex++
		totalTags += len(values)
		if totalTags > b.QueryTagsLimit {
			// too many tags, fail everything
			return "", nil, ErrTooManyTagValues
		}
	}

	if filter.Since != nil {
		conditions = append(conditions, `event.created_at >= ?`)
		whereParams = append(whereParams, filter.Since)
	}
	if filter.Until != nil {
		conditions = append(conditions, `event.created_at <= ?`)
		whereParams = append(whereParams, filter.Until)
	}
	if filter.Search != "" {
		conditions = append(conditions, `event.content LIKE ? ESCAPE '\'`)
		whereParams = append(whereParams, `%`+strings.ReplaceAll(filter.Search, `%`, `\%`)+`%`)
	}

	if len(conditions) == 0 {
		// fallback
		conditions = append(conditions, `true`)
	}

	// Combine params in the correct order: JOIN params first, then WHERE params, then LIMIT
	params := make([]any, 0, len(joinParams)+len(whereParams)+1)
	params = append(params, joinParams...)
	params = append(params, whereParams...)

	if filter.Limit < 1 || filter.Limit > b.QueryLimit {
		params = append(params, b.QueryLimit)
	} else {
		params = append(params, filter.Limit)
	}

	// Build the FROM clause with joins
	fromClause := "event"
	if len(joins) > 0 {
		fromClause = "event " + strings.Join(joins, " ")
	}

	var query string
	if doCount {
		// For COUNT with joins, use COUNT(DISTINCT) to avoid counting duplicates
		if len(joins) > 0 {
			query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT COUNT(DISTINCT event.id)
        FROM `+fromClause+` WHERE `+
				strings.Join(conditions, " AND ")+
				" LIMIT ?")
		} else {
			query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT COUNT(*)
        FROM `+fromClause+` WHERE `+
				strings.Join(conditions, " AND ")+
				" LIMIT ?")
		}
	} else {
		// For SELECT with joins, use GROUP BY instead of DISTINCT for better performance
		if len(joins) > 0 {
			query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
        FROM `+fromClause+` WHERE `+
				strings.Join(conditions, " AND ")+
				` GROUP BY event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
        ORDER BY event.created_at DESC, event.id LIMIT ?`)
		} else {
			query = sqlx.Rebind(sqlx.BindType("sqlite3"), `SELECT event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
        FROM `+fromClause+` WHERE `+
				strings.Join(conditions, " AND ")+
				" ORDER BY event.created_at DESC, event.id LIMIT ?")
		}
	}

	return query, params, nil
}
