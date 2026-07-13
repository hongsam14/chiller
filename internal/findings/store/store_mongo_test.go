//go:build integration

// Integration test for the durable Mongo FindingsStore. It runs the EXACT same
// contract suite as the in-memory impl, so the backend swap is proven to reproduce
// identical observable behavior — in particular the order preservation and the
// tail truncation, neither of which a durable store gets for free. Requires Docker;
// excluded from the default build via the `integration` tag (Q10=A).
package store_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/crypto"
	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/store"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func testKeyring(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i * 3)
	}
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func mongoURI(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("start mongo container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return uri
}

func TestMongo_FindingsStoreContract(t *testing.T) {
	uri := mongoURI(t)
	ctx := context.Background()

	var n int64
	runFindingsStoreContract(t, func() store.FindingsStore {
		// Each sub-test gets a fresh database so their fixtures never collide.
		db := "chiller_test_" + strconv.FormatInt(atomic.AddInt64(&n, 1), 10)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		s, err := store.NewMongo(cctx, uri, db, testKeyring(t), 0)
		if err != nil {
			t.Fatalf("NewMongo: %v", err)
		}
		return s
	})
}

// TestMongo_EvidenceIsEncryptedAtRest reads the raw BSON and asserts the evidence
// is not sitting in it in the clear. This collection is where a scan's response
// fragments live for the long term, so "we encrypt it" needs to be checked against
// the bytes, not against the round-trip (which would pass either way).
func TestMongo_EvidenceIsEncryptedAtRest(t *testing.T) {
	uri := mongoURI(t)
	ctx := context.Background()

	const secret = "Authorization: Bearer super-secret-token"
	s, err := store.NewMongo(ctx, uri, "chiller_enc", testKeyring(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	f := domain.Finding{
		CWEs: []domain.CWE{"CWE-614"}, TargetURL: "https://x.test/y", SourceTool: "obs",
		Severity: domain.SevCritical, Evidence: domain.Evidence{Snippets: []string{secret}},
	}
	if err := s.SaveFindings(ctx, "scan-1", []domain.Finding{f}); err != nil {
		t.Fatal(err)
	}

	// The seam still yields plaintext.
	got, err := s.LoadFindings(ctx, "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Evidence.Snippets[0] != secret {
		t.Fatalf("evidence did not round-trip: %+v", got)
	}

	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Disconnect(ctx) }()
	raw, err := cli.Database("chiller_enc").Collection("findings").
		FindOne(ctx, bson.M{"scanId": "scan-1"}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("super-secret")) {
		t.Fatal("the finding document contains its evidence in PLAINTEXT")
	}
	// The queryable fields, on the other hand, must stay readable — they are what
	// the indexes are on.
	if !bytes.Contains(raw, []byte("https://x.test/y")) {
		t.Error("the target URL should NOT be encrypted: it is indexed and queried")
	}
	// And the severity is stored by name, not as an ordinal (G2).
	if !bytes.Contains(raw, []byte("critical")) {
		t.Error("severity should be persisted as its name, not its iota ordinal")
	}
}

// TestMongo_TTLIndexOnlyWhenAsked pins the retention default (Q7=C): findings are
// kept indefinitely unless an operator opts into a TTL.
func TestMongo_TTLIndexOnlyWhenAsked(t *testing.T) {
	uri := mongoURI(t)
	ctx := context.Background()

	hasTTL := func(db string, ttl time.Duration) bool {
		if _, err := store.NewMongo(ctx, uri, db, testKeyring(t), ttl); err != nil {
			t.Fatal(err)
		}
		cli, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cli.Disconnect(ctx) }()
		cur, err := cli.Database(db).Collection("findings").Indexes().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var idx []bson.M
		if err := cur.All(ctx, &idx); err != nil {
			t.Fatal(err)
		}
		for _, i := range idx {
			if _, ok := i["expireAfterSeconds"]; ok {
				return true
			}
		}
		return false
	}

	if hasTTL("chiller_nottl", 0) {
		t.Error("findings must be kept indefinitely by default (Q7=C); a TTL index appeared unasked")
	}
	if !hasTTL("chiller_ttl", 24*time.Hour) {
		t.Error("--findings-ttl did not create the TTL index")
	}
}
