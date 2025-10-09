# Slow Query Analysis: Kind 1984 Reports

## The Slow Query (638ms)

```json
{
  "kinds": [1984],
  "authors": [
    "58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196",
    "7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95",
    "863f2c555276e9ed738933b0efee6b021042f16e1529dd755704885b87fee183",
    "c9f4f6847f2c8c76964e37a2c26f2d3a153f9d4e7231e5705c8952b6345c292a",
    "f275ab37d64f6be0379a85ce5736060e164b136a08c7f1e78dea633abad84bfe"
  ],
  "since": 1759969515,
  "#p": [
    "35d2cbc223ef181b9368048a8117b8b55b1ef676b78170786203970c6e0d079a",
    "6d5a6d6c148ac36b07bb277a6f711faca177675a2f3dc0fedd73d39b473d15c6",
    "802b590f4e6d3c0329a6c4baef0b0bae33301f2879e1b3111488c5c8a493697e",
    "8230c6222dea501c168d871de40d3ced4946b5608683af486a22e55426642641"
  ]
}
```

## Generated SQL

```sql
SELECT event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
FROM event 
INNER JOIN tag AS tag0 ON tag0.event_id = event.id 
    AND tag0.identifier = 'p' 
    AND tag0.first_data IN (
        '35d2cbc223ef181b9368048a8117b8b55b1ef676b78170786203970c6e0d079a',
        '6d5a6d6c148ac36b07bb277a6f711faca177675a2f3dc0fedd73d39b473d15c6',
        '802b590f4e6d3c0329a6c4baef0b0bae33301f2879e1b3111488c5c8a493697e',
        '8230c6222dea501c168d871de40d3ced4946b5608683af486a22e55426642641'
    )
WHERE event.pubkey IN (
        '58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196',
        '7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95',
        '863f2c555276e9ed738933b0efee6b021042f16e1529dd755704885b87fee183',
        'c9f4f6847f2c8c76964e37a2c26f2d3a153f9d4e7231e5705c8952b6345c292a',
        'f275ab37d64f6be0379a85ce5736060e164b136a08c7f1e78dea633abad84bfe'
    )
  AND event.kind IN (1984) 
  AND event.created_at >= 1759969515
GROUP BY event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
ORDER BY event.created_at DESC, event.id 
LIMIT 100
```

## Problem Before Migration 9

Without the specialized indexes, SQLite's query planner might:

1. **Start with tag JOIN** - Scan `tag` table for `identifier='p'` and matching values
2. **Filter by authors** - Check if event.pubkey is in the 5-author list
3. **Filter by kind** - Check if kind=1984
4. **Filter by time** - Check if created_at >= timestamp
5. **Return 0 results** after scanning many rows

This is inefficient, especially when no matching events exist (0 results).

## Solution: Migration 9 Indexes

### Index 1: Partial index for kind 1984
```sql
CREATE INDEX event_kind1984_pubkey_time_idx 
ON event(pubkey, created_at DESC, id) WHERE kind = 1984
```

**Benefits:**
- Only indexes kind 1984 events (small, fast)
- Allows fast lookup by pubkey IN (...)
- Maintains time ordering for `since` filter
- SQLite can quickly determine if ANY of the 5 authors have kind 1984 events

### Index 2: Composite covering index
```sql
CREATE INDEX event_pubkey_kind_time_id_idx 
ON event(pubkey, kind, created_at DESC, id)
```

**Benefits:**
- Covers all columns needed for author+kind+time queries
- No table lookups needed (covering index)
- Supports efficient ORDER BY

## Optimized Query Plan

With Migration 9, SQLite should now:

1. **Use partial index** - Quickly find kind=1984 events for the 5 authors
2. **Apply time filter** - Use the time ordering in the index
3. **Join with tags** - Only join the small result set with tag table
4. **Return quickly** - If no events match authors+kind+time, return immediately

Expected improvement: **638ms → 10-50ms**

## Comparison: Without Tag JOIN

For reference, the same query WITHOUT the `#p` tag filter:

```sql
SELECT event.id, event.pubkey, event.created_at, event.kind, event.tags, event.content, event.sig
FROM event 
WHERE event.pubkey IN (?, ?) 
  AND event.kind IN (1984) 
  AND event.created_at >= ?
ORDER BY event.created_at DESC, event.id 
LIMIT 100
```

This is much simpler and would be very fast with the partial index.

The tag JOIN is the expensive part, which is why the optimization focuses on reducing the result set BEFORE the JOIN.

## Testing

Run the test to see the generated SQL:

```bash
go test -v ./pkg/newsqlite3 -run TestQueryEventsSql_Kind1984WithManyAuthorsAndTags
```

## Monitoring

After deploying Migration 9, monitor:
- Slow query logs for kind 1984 queries
- Average query time in `/stats` endpoint
- Database performance metrics

The slow queries should drop significantly for this pattern.

