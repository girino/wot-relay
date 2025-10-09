# Anti-Sync Bots Filter Configuration

## Overview

The anti-sync bots filter can now be optionally enabled or disabled via environment variable.

## Environment Variable

**Variable:** `ENABLE_ANTI_SYNC_BOTS_FILTER` *(optional)*

**Values:**
- `TRUE` - Enable anti-sync bots filter (requires auth and WoT membership)
- `FALSE` or *not set* - Disable anti-sync bots filter (default)

**Default:** Disabled (if variable is not present or set to anything other than `TRUE`)

## What the Filter Does (When Enabled)

The anti-sync bots filter detects and blocks automated scrapers trying to download all events.

### Detection Criteria (from `policies.AntiSyncBots`)
The filter triggers when a query matches sync bot patterns, such as:
- Large limit values (e.g., limit > 200)
- Queries without specific filters (broad scraping)
- Patterns typical of data collection bots

### When Triggered
If a query matches sync bot patterns:
1. **Check authentication**
   - If not authenticated → Reject with "auth-required: kind 1 scraping requires authentication"

2. **Check Web of Trust membership**
   - If authenticated but not in WoT → Reject with "{pubkey} not in web of trust"

3. **Allow if authenticated AND in WoT**
   - Trusted users can run large queries

## Configuration

### In .env file:

```bash
# Option 1: Don't set the variable at all (default - filter disabled)
# (no ENABLE_ANTI_SYNC_BOTS_FILTER line needed)

# Option 2: Explicitly disable (same as not setting it)
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE

# Option 3: Enable anti-sync bots filter (requires auth and WoT)
ENABLE_ANTI_SYNC_BOTS_FILTER=TRUE
```

### Startup Log

When the relay starts, you'll see:
```
[INFO] [CONFIG] Anti-sync bots filter disabled
```

or

```
[INFO] [CONFIG] Anti-sync bots filter enabled
```

## Use Cases

### Disabled (Default)
- ✅ All queries allowed (no bot detection)
- ✅ No authentication required for large queries
- ✅ Open access for legitimate bulk data users
- ⚠️ Vulnerable to scraping/data collection

**Best for:**
- Public relays that want open data access
- Development/testing environments
- Relays that prefer other rate limiting mechanisms
- Relays with good query optimization

### Enabled
- ✅ Detects and blocks sync bot patterns
- ✅ Only authenticated WoT members can run large queries
- ✅ Protects against unauthorized scraping
- ⚠️ May block legitimate bulk data users

**Best for:**
- Private/semi-private relays
- Relays wanting to prevent data scraping
- Relays with limited bandwidth/resources
- WoT-focused relays

## Impact on Queries

### With Filter Disabled (Default)
```bash
# All queries work without authentication:

# Large limit query
{"kinds": [1], "limit": 500}
✅ Allowed

# Broad query
{"since": 1234567890, "limit": 1000}
✅ Allowed
```

### With Filter Enabled
```bash
# Small queries still work
{"kinds": [1], "authors": ["abc..."], "limit": 50}
✅ Allowed (not detected as sync bot)

# Large queries without auth
{"kinds": [1], "limit": 500}
❌ Rejected: "auth-required: kind 1 scraping requires authentication"

# Large queries with auth but not in WoT
{"kinds": [1], "limit": 500}
(with auth)
❌ Rejected: "{pubkey} not in web of trust"

# Large queries with auth AND in WoT
{"kinds": [1], "limit": 500}
(with auth + WoT)
✅ Allowed
```

## Related Configuration

Works together with:
- `ENABLE_KIND1984_FILTER` - Controls kind 1984 report filters
- Rate limiters:
  - `EventIPRateLimiter` (50 events/min per IP, cap 300)
  - `EventPubKeyRateLimiter` (10 events/min per pubkey, cap 60)
  - `FilterIPRateLimiter` (20 queries/min per IP, cap 100)
  - `ConnectionRateLimiter` (10 connections per 2min per IP, cap 30)

## Why Default to Disabled?

1. **Open by default** - Aligns with nostr philosophy of open data
2. **Legitimate use cases** - Many tools need bulk data access
3. **Other protections** - Rate limiters still active
4. **Query optimization** - Migration 9 + index hints handle large queries efficiently
5. **Opt-in restriction** - Relays can choose to enable if needed

## Testing

### Test Filter Status
```bash
# Start relay and check logs
./wot-relay

# Look for:
[INFO] [CONFIG] Anti-sync bots filter disabled
# or
[INFO] [CONFIG] Anti-sync bots filter enabled
```

### Test Behavior (Filter Disabled)
```bash
# All these should work without auth:
{"kinds": [1], "limit": 500}
{"since": 1234567890, "limit": 1000}
{"kinds": [1, 6, 7], "limit": 300}
```

### Test Behavior (Filter Enabled)
```bash
# Without auth - should fail:
{"kinds": [1], "limit": 500}
→ "auth-required: kind 1 scraping requires authentication"

# With auth but not in WoT - should fail:
(authenticated but pubkey not in trust network)
→ "{pubkey} not in web of trust"

# With auth AND in WoT - should work:
(authenticated + WoT member)
→ Query succeeds
```

## Recommendations

### For Most Public Relays (Default)
```bash
# Don't set the variable (disabled)
```
- Open data access
- Let rate limiters handle abuse
- Monitor via `/stats` endpoint

### For Private/WoT Relays
```bash
ENABLE_ANTI_SYNC_BOTS_FILTER=TRUE
```
- Restrict bulk queries to WoT members
- Prevent unauthorized scraping
- Combine with `ENABLE_KIND1984_FILTER=TRUE` for full protection

### For Mixed Approach
```bash
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE
ENABLE_KIND1984_FILTER=TRUE
```
- Open bulk data access
- But restrict sensitive report queries
- Balance between openness and privacy

## Summary

| Setting | Bulk Queries | Kind 1984 | Auth Required |
|---------|--------------|-----------|---------------|
| Both OFF (default) | ✅ Open | ✅ Open | ❌ No |
| AntiSyncBots ON | 🔒 WoT only | ✅ Open | ✅ For bulk |
| Kind1984 ON | ✅ Open | 🔒 WoT only | ✅ For reports |
| Both ON | 🔒 WoT only | 🔒 WoT only | ✅ For both |

**Recommended for most relays:** Both OFF (fully open with optimization)

