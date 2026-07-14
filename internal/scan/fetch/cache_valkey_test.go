//go:build integration

// Integration test for the durable Valkey ResponseCache. It runs the EXACT same
// contract suite as the in-memory impl, so the backend swap is proven to reproduce
// identical observable behavior — including that WipeScan really removes a scan's
// keys, which is the whole point of FR-12 and the one thing an in-memory cache
// gets for free by exiting. Requires Docker; excluded via the `integration` tag.
package fetch_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/crypto"
	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/fetch"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func testKeyring(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i * 5)
	}
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func valkeyAddr(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	opt, err := redis.ParseURL(uri)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return opt.Addr
}

func TestValkey_ResponseCacheContract(t *testing.T) {
	addr := valkeyAddr(t)
	ctx := context.Background()

	db := 0
	runResponseCacheContract(t, func() fetch.ResponseCache {
		// Each sub-test gets its own logical DB so their fixtures never collide.
		db++
		c, err := fetch.NewValkeyCache(ctx, addr, testKeyring(t), fetch.ValkeyOptions{DB: db})
		if err != nil {
			t.Fatalf("NewValkeyCache: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	})
}

// TestValkey_CachedResponseIsEncryptedAtRest reads the raw value out of Valkey and
// asserts the response body and its session cookie are not in it. A cached response
// is the most sensitive thing this system holds: the whole body, the headers, and
// the target's session cookies.
func TestValkey_CachedResponseIsEncryptedAtRest(t *testing.T) {
	addr := valkeyAddr(t)
	ctx := context.Background()

	const body = "<html>account balance: 12345</html>"
	const cookie = "SESSION=super-secret-session"

	c, err := fetch.NewValkeyCache(ctx, addr, testKeyring(t), fetch.ValkeyOptions{DB: 9})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	url := "https://bank.test/account"
	e := &domain.HAREntry{
		URL: url, Scheme: "https", Status: 200,
		Cookies: []domain.Cookie{{Name: "SESSION", Value: "super-secret-session"}},
		Body:    []byte(body),
	}
	if err := c.Put(ctx, "scan-1", url, e); err != nil {
		t.Fatal(err)
	}

	// The seam still yields plaintext — this is why the observation signatures can
	// keep regexing the body without knowing the cache is encrypted at all.
	got, hit, err := c.Get(ctx, "scan-1", url)
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if string(got.Body) != body {
		t.Fatalf("body did not round-trip")
	}

	// The bytes in Valkey do not.
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 9})
	defer func() { _ = rdb.Close() }()
	keys, err := rdb.Keys(ctx, "chiller:cache:scan-1:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("nothing was written to Valkey")
	}
	for _, k := range keys {
		raw, err := rdb.Get(ctx, k).Result()
		if err != nil {
			continue // the index key is a SET, not a string
		}
		if contains(raw, body) || contains(raw, cookie) || contains(raw, "12345") {
			t.Fatalf("key %s holds the response in PLAINTEXT", k)
		}
	}
}

// TestValkey_TTLIsSetOnEveryKey pins the backstop. WipeScan is what normally
// removes a scan's responses; the TTL is what removes them when the process dies
// before it can wipe — without it a crashed scan would leave bodies in Valkey
// forever.
func TestValkey_TTLIsSetOnEveryKey(t *testing.T) {
	addr := valkeyAddr(t)
	ctx := context.Background()

	c, err := fetch.NewValkeyCache(ctx, addr, testKeyring(t), fetch.ValkeyOptions{DB: 10, TTL: 90 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	url := "https://x.test/a"
	if err := c.Put(ctx, "scan-ttl", url, &domain.HAREntry{URL: url, Body: []byte("x")}); err != nil {
		t.Fatal(err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 10})
	defer func() { _ = rdb.Close() }()
	keys, err := rdb.Keys(ctx, "chiller:cache:scan-ttl:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 { // the entry + the scan's key index
		t.Fatalf("expected the entry and its index key, got %v", keys)
	}
	for _, k := range keys {
		ttl, err := rdb.TTL(ctx, k).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttl <= 0 {
			t.Errorf("key %s has no TTL — a crashed scan would leave it in Valkey forever", k)
		}
		if ttl > 90*time.Minute {
			t.Errorf("key %s TTL %v exceeds the configured 90m", k, ttl)
		}
	}
}

// TestValkey_WipeRemovesEveryKey is the durable half of the FR-12 regression: the
// in-memory cache "wipes" by dropping a map, which proves nothing about a backend
// that survives the process.
func TestValkey_WipeRemovesEveryKey(t *testing.T) {
	addr := valkeyAddr(t)
	ctx := context.Background()

	c, err := fetch.NewValkeyCache(ctx, addr, testKeyring(t), fetch.ValkeyOptions{DB: 11})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	for _, u := range []string{"https://x.test/1", "https://x.test/2", "https://x.test/3"} {
		if err := c.Put(ctx, "scan-w", u, &domain.HAREntry{URL: u, Body: []byte("body")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.WipeScan(ctx, "scan-w"); err != nil {
		t.Fatal(err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 11})
	defer func() { _ = rdb.Close() }()
	keys, err := rdb.Keys(ctx, "chiller:cache:scan-w:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("%d keys survived the wipe: %v", len(keys), keys)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		bytesIndex(haystack, needle) >= 0
}

func bytesIndex(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
