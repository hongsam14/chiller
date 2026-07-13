// cache_valkey.go is the durable ResponseCache, added beside InMemory behind the
// identical ResponseCache interface. It is what makes the single-fetch reuse
// (FR-02, US-002) actually work once workers are separate processes: an in-memory
// cache is per-process, so with a worker pool each URL was being fetched once per
// worker that happened to draw a checker for it.
//
// It runs on its OWN Valkey instance, not the one behind the work queue. The two
// want opposite policies — the queue must never evict (a dropped work item is a
// lost item) while the cache must be free to evict under memory pressure (a
// dropped entry just means a refetch) — and sharing one instance means one of those
// policies is wrong. Either the cache's LRU starts evicting queue entries, or the
// queue's noeviction turns a full cache into write errors.
//
// Values are encrypted (Q6=A). A cached response is the most sensitive thing this
// system holds in flight: the whole body, the headers, and the target's session
// cookies. The encryption is entirely inside this file — Put seals, Get opens — so
// a checker still receives an ordinary plaintext *domain.HAREntry and the
// observation signatures run their regexes over it unchanged. Valkey only ever
// sees opaque bytes, in memory, in its RDB file and in any backup taken of it.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/enki-offen/chiller/internal/crypto"
	"github.com/enki-offen/chiller/internal/domain"
	"github.com/redis/go-redis/v9"
)

// DefaultCacheTTL bounds how long a cached response can outlive its scan. WipeScan
// is what normally removes them; this is the backstop for the case that matters —
// a process that dies before it can wipe. Without it, a crashed scan would leave
// response bodies in Valkey indefinitely.
const DefaultCacheTTL = 6 * time.Hour

// Valkey is the durable ResponseCache. Construct with NewValkeyCache.
type Valkey struct {
	rdb *redis.Client
	kr  crypto.Keyring
	ttl time.Duration
}

var _ ResponseCache = (*Valkey)(nil)

// ValkeyOptions configure the cache client.
type ValkeyOptions struct {
	Password string
	DB       int
	TTL      time.Duration // 0 ⇒ DefaultCacheTTL
}

// NewValkeyCache connects to the cache instance and verifies it (fail-closed).
func NewValkeyCache(ctx context.Context, addr string, kr crypto.Keyring, opts ValkeyOptions) (*Valkey, error) {
	if kr == nil {
		return nil, errors.New("fetch: valkey cache needs a keyring (cached responses are encrypted)")
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: opts.Password, DB: opts.DB})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("fetch: valkey cache ping (fail-closed): %w", err)
	}
	return &Valkey{rdb: rdb, kr: kr, ttl: ttl}, nil
}

// Ping reports backend reachability for the deep health check.
func (c *Valkey) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Close releases the client.
func (c *Valkey) Close() error { return c.rdb.Close() }

// entryKey is the key one cached response lives under. The URL is hashed, not
// embedded: URLs can be long, can contain any byte, and are looked up only by
// exact match — nothing scans this keyspace by pattern.
func entryKey(scanID domain.ScanID, url string) string {
	sum := sha256.Sum256([]byte(url))
	return fmt.Sprintf("chiller:cache:%s:%s", scanID, hex.EncodeToString(sum[:]))
}

// indexKey is the set of every entry key belonging to a scan. WipeScan needs to
// delete exactly a scan's keys, and the alternative — SCAN with a match pattern —
// walks the entire keyspace, which is O(everything) and gets slower precisely as
// the cache gets busier.
func indexKey(scanID domain.ScanID) string {
	return fmt.Sprintf("chiller:cache:%s:keys", scanID)
}

func (c *Valkey) Put(ctx context.Context, scanID domain.ScanID, url string, e *domain.HAREntry) error {
	if e == nil {
		return nil
	}
	pt, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("fetch: cache marshal %s: %w", url, err)
	}
	env, err := crypto.Seal(c.kr, pt, crypto.CacheAAD(string(scanID), url))
	if err != nil {
		return fmt.Errorf("fetch: cache seal %s: %w", url, err)
	}
	blob, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("fetch: cache marshal envelope %s: %w", url, err)
	}

	k, idx := entryKey(scanID, url), indexKey(scanID)
	pipe := c.rdb.TxPipeline()
	pipe.Set(ctx, k, blob, c.ttl)
	pipe.SAdd(ctx, idx, k)
	pipe.Expire(ctx, idx, c.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("fetch: cache put %s: %w", url, err)
	}
	return nil
}

func (c *Valkey) Get(ctx context.Context, scanID domain.ScanID, url string) (*domain.HAREntry, bool, error) {
	blob, err := c.rdb.Get(ctx, entryKey(scanID, url)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("fetch: cache get %s: %w", url, err)
	}
	var env crypto.Envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, false, fmt.Errorf("fetch: cache unmarshal envelope %s: %w", url, err)
	}
	// A value that will not open is a real error, not a miss: it means the entry
	// was tampered with, was sealed for a different URL, or was written under a key
	// we no longer hold. Reporting it as a miss would refetch and paper over it.
	pt, err := crypto.Open(c.kr, env, crypto.CacheAAD(string(scanID), url))
	if err != nil {
		return nil, false, fmt.Errorf("fetch: cache open %s: %w", url, err)
	}
	var e domain.HAREntry
	if err := json.Unmarshal(pt, &e); err != nil {
		return nil, false, fmt.Errorf("fetch: cache unmarshal %s: %w", url, err)
	}
	return &e, true, nil
}

// WipeScan deletes a scan's cached responses (FR-12/NFR-16). It reads the scan's
// key set and deletes exactly those keys, rather than walking the keyspace.
//
// The index set is itself a normal key, and this instance runs allkeys-lru, so
// eviction is free to take it while the entries it points at survive. That would
// make the wipe silently delete nothing — and report success — precisely when the
// cache is under memory pressure. So when the index comes back empty, fall back to
// a SCAN over the scan's key prefix. The scan is O(keyspace) and is why the index
// exists at all, but it only runs when the index is gone, which is exactly when
// correctness has to win over cost.
func (c *Valkey) WipeScan(ctx context.Context, scanID domain.ScanID) error {
	idx := indexKey(scanID)
	keys, err := c.rdb.SMembers(ctx, idx).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("fetch: cache wipe %s: list keys: %w", scanID, err)
	}
	if len(keys) == 0 {
		keys, err = c.scanKeys(ctx, scanID)
		if err != nil {
			return err
		}
	}
	pipe := c.rdb.TxPipeline()
	if len(keys) > 0 {
		pipe.Del(ctx, keys...)
	}
	pipe.Del(ctx, idx)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("fetch: cache wipe %s: %w", scanID, err)
	}
	return nil
}

// scanKeys enumerates a scan's entry keys the slow way, for when the index set was
// evicted.
func (c *Valkey) scanKeys(ctx context.Context, scanID domain.ScanID) ([]string, error) {
	var (
		out    []string
		cursor uint64
	)
	pattern := fmt.Sprintf("chiller:cache:%s:*", scanID)
	for {
		batch, next, err := c.rdb.Scan(ctx, cursor, pattern, 256).Result()
		if err != nil {
			return nil, fmt.Errorf("fetch: cache wipe %s: scan: %w", scanID, err)
		}
		out = append(out, batch...)
		if next == 0 {
			return out, nil
		}
		cursor = next
	}
}
