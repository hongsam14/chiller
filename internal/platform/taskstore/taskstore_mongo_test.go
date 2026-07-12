//go:build integration

// Integration test for the durable Mongo TaskStore. It runs the EXACT same
// contract suite as the mem impl (F-2/PR-11a), so the backend swap is proven to
// reproduce identical observable behavior — including the findOneAndUpdate CAS
// single-winner semantics. Requires Docker; excluded from the default build via
// the `integration` tag (Q10=A).
package taskstore_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
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
		s, err := taskstore.NewMongo(cctx, uri, db)
		if err != nil {
			t.Fatalf("NewMongo: %v", err)
		}
		return s
	})
}
