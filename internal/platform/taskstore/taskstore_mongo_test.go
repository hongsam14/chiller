//go:build integration

// Integration test for the durable Mongo TaskStore. It runs the EXACT same
// contract suite as the mem impl (F-2/PR-11a), so the backend swap is proven to
// reproduce identical observable behavior — including the findOneAndUpdate CAS
// single-winner semantics. Requires Docker; excluded from the default build via
// the `integration` tag (Q10=A).
package taskstore_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enki-offen/chiller/internal/crypto"
	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestMongo_TaskStoreContract(t *testing.T) {
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

	var n int64
	runTaskStoreContract(t, func() platform.TaskStore {
		// Each sub-test gets a fresh database so their fixtures never collide.
		db := "chiller_test_" + strconv.FormatInt(atomic.AddInt64(&n, 1), 10)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		s, err := taskstore.NewMongo(cctx, uri, db, testKeyring(t))
		if err != nil {
			t.Fatalf("NewMongo: %v", err)
		}
		return s
	})
}

// testKeyring is the key material the durable ledger seals evidence with. The
// content does not matter; that there IS one does — NewMongo refuses to start
// without a key, because the ledger carries finding evidence.
func testKeyring(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i * 7)
	}
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

// TestMongo_LedgerEvidenceIsEncryptedAtRest reads the RAW BSON of a ledger
// document and asserts the plaintext evidence is not in it. The ledger is how a
// worker's findings reach the manager, so without this it is a second durable home
// for every response fragment — the copy that encrypting the findings collection
// alone would have left sitting next to it in the clear.
func TestMongo_LedgerEvidenceIsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	ctr, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("start mongo container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}

	const secret = "Set-Cookie: SESSION=super-secret-value"
	s, err := taskstore.NewMongo(ctx, uri, "chiller_enc", testKeyring(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveScan(ctx, platform.Scan{ID: "s", State: platform.StateRunning}); err != nil {
		t.Fatal(err)
	}
	item := domain.WorkItem{ID: "s#0", ScanID: "s", Scope: domain.URLScoped}
	if err := s.AppendWorkItems(ctx, "s", []domain.WorkItem{item}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimItem(ctx, "s#0", platform.Lease{Owner: "w", Expiry: 1 << 40}, 0); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	f := domain.Finding{
		CWEs: []domain.CWE{"CWE-614"}, TargetURL: "https://x/y", SourceTool: "obs",
		Severity: domain.SevHigh, Evidence: domain.Evidence{Snippets: []string{secret}},
	}
	if ok, err := s.RecordOutcome(ctx, "s#0", "w", domain.WorkItemOutcome{ID: "s#0", Findings: []domain.Finding{f}}); err != nil || !ok {
		t.Fatalf("record: %v %v", ok, err)
	}

	// Round-trip through the seam still yields the plaintext...
	got, err := s.AllOutcomes(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Findings) != 1 || got[0].Findings[0].Evidence.Snippets[0] != secret {
		t.Fatalf("evidence did not round-trip: %+v", got)
	}

	// ...but the bytes on disk do not contain it.
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Disconnect(ctx) }()
	raw, err := cli.Database("chiller_enc").Collection("workledger").FindOne(ctx, bson.M{"_id": "s#0"}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("the ledger document contains the evidence in PLAINTEXT")
	}
	if bytes.Contains(raw, []byte("super-secret")) {
		t.Fatal("the ledger document leaks part of the evidence")
	}

	// And after a purge, the envelope is gone entirely.
	if err := s.PurgeOutcomeEvidence(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	after, err := s.AllOutcomes(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(after[0].Findings[0].Evidence.Snippets) != 0 {
		t.Error("PurgeOutcomeEvidence left the evidence behind")
	}
}
