// taskstore_mongo.go is the durable TaskStore (Q5=A/Q6=B/Q12=B), added beside
// the in-memory Mem behind the identical platform.TaskStore interface (PR-11):
// the orchestrator never learns which one it got. It persists the Scan
// aggregate, the WorkItem ledger, and the report in three collections; the two
// CAS transitions use findOneAndUpdate with an expected-state filter for
// single-document atomicity (PR-4 single-winner claim), so no multi-document
// transaction is needed. Writes use write-concern majority (RPO≈0, Q12=B).
package taskstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/enki-offen/chiller/internal/crypto"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/report"
	"github.com/enki-offen/chiller/internal/platform"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

// Mongo is the durable TaskStore. Construct with NewMongo.
type Mongo struct {
	client  *mongo.Client
	scans   *mongo.Collection
	ledger  *mongo.Collection
	reports *mongo.Collection
	kr      crypto.Keyring
}

var _ platform.TaskStore = (*Mongo)(nil)

// reportDoc wraps a report so it can be stored under the scan's id.
type reportDoc struct {
	ID     domain.ScanID `bson:"_id"`
	Report report.Report `bson:"report"`
}

// The ledger's stored shape differs from the in-memory one in exactly one way:
// a finding's evidence is an encrypted envelope rather than plaintext snippets.
// That forces a local document type — platform.WorkLedgerEntry embeds a
// domain.WorkItemOutcome, which carries evidence in the clear.
type ledgerDoc struct {
	ID          domain.WorkItemID  `bson:"_id"`
	ScanID      domain.ScanID      `bson:"scanId"`
	Item        domain.WorkItem    `bson:"item"`
	State       platform.ItemState `bson:"state"`
	Outcome     *sealedOutcome     `bson:"outcome"`
	LeaseOwner  string             `bson:"leaseOwner"`
	LeaseExpiry domain.Timestamp   `bson:"leaseExpiry"`
}

type sealedOutcome struct {
	ID            domain.WorkItemID                 `bson:"id"`
	Findings      []sealedFinding                   `bson:"findings"`
	Failed        []domain.CheckerID                `bson:"failed"`
	NotApplicable map[domain.CheckerID][]domain.CWE `bson:"notApplicable"`
}

type sealedFinding struct {
	CWEs       []domain.CWE    `bson:"cwes"`
	TargetURL  string          `bson:"targetUrl"`
	SourceTool string          `bson:"sourceTool"`
	Severity   domain.Severity `bson:"severity"`
	Evidence   crypto.Envelope `bson:"evidence"`
}

// NewMongo connects to Mongo (TLS enforced via the URI, SECURITY-01) with
// majority write-concern, verifies connectivity (fail-closed, SECURITY-15), and
// ensures the required indexes. dbName defaults to "chiller".
func NewMongo(ctx context.Context, uri, dbName string, kr crypto.Keyring) (*Mongo, error) {
	if kr == nil {
		return nil, errors.New("taskstore: mongo needs a keyring (the ledger carries finding evidence)")
	}
	if dbName == "" {
		dbName = "chiller"
	}
	opts := options.Client().ApplyURI(uri).SetWriteConcern(writeconcern.Majority())
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("taskstore: mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("taskstore: mongo ping (fail-closed): %w", err)
	}
	db := client.Database(dbName)
	m := &Mongo{
		client:  client,
		kr:      kr,
		scans:   db.Collection("scans"),
		ledger:  db.Collection("workledger"),
		reports: db.Collection("reports"),
	}
	if err := m.ensureIndexes(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return m, nil
}

func (m *Mongo) ensureIndexes(ctx context.Context) error {
	if _, err := m.scans.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "state", Value: 1}},
	}); err != nil {
		return fmt.Errorf("taskstore: index scans.state: %w", err)
	}
	if _, err := m.ledger.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "scanId", Value: 1}, {Key: "state", Value: 1}},
	}); err != nil {
		return fmt.Errorf("taskstore: index workledger.scanId_state: %w", err)
	}
	// Serves ListExpiredInflight ({scanId, state:inflight, leaseExpiry<now}).
	if _, err := m.ledger.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "scanId", Value: 1}, {Key: "state", Value: 1}, {Key: "leaseExpiry", Value: 1}},
	}); err != nil {
		return fmt.Errorf("taskstore: index workledger.scanId_state_leaseExpiry: %w", err)
	}
	return nil
}

// Close disconnects the client. Not part of the seam; called by the composition
// root on shutdown.
func (m *Mongo) Close(ctx context.Context) error { return m.client.Disconnect(ctx) }

// Ping verifies connectivity for the deep health check (RESILIENCY-06).
func (m *Mongo) Ping(ctx context.Context) error { return m.client.Ping(ctx, nil) }

func (m *Mongo) SaveScan(ctx context.Context, s platform.Scan) error {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	_, err := m.scans.ReplaceOne(ctx, bson.M{"_id": s.ID}, s, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("taskstore: SaveScan %q: %w", s.ID, err)
	}
	return nil
}

func (m *Mongo) LoadScan(ctx context.Context, id domain.ScanID) (platform.Scan, error) {
	var s platform.Scan
	err := m.scans.FindOne(ctx, bson.M{"_id": id}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return platform.Scan{}, fmt.Errorf("taskstore: scan %q not found", id)
	}
	if err != nil {
		return platform.Scan{}, fmt.Errorf("taskstore: LoadScan %q: %w", id, err)
	}
	return s, nil
}

// CompareAndSetState atomically swaps state expected→next on a single scan
// document. It mirrors the mem contract exactly: a missing scan is an error, and
// (false, nil) means only "the scan exists but its state was not `expected`" —
// a lost race. Distinguishing the two costs one extra read on the no-match path.
func (m *Mongo) CompareAndSetState(ctx context.Context, id domain.ScanID, expected, next platform.ScanState) (bool, error) {
	res := m.scans.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "state": expected.String()},
		bson.M{"$set": bson.M{"state": next}},
	)
	if errors.Is(res.Err(), mongo.ErrNoDocuments) {
		exists, err := m.exists(ctx, m.scans, id)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("taskstore: scan %q not found", id)
		}
		return false, nil // exists but wrong expected state → lost race
	}
	if res.Err() != nil {
		return false, fmt.Errorf("taskstore: CAS state %q: %w", id, res.Err())
	}
	return true, nil
}

func (m *Mongo) AppendWorkItems(ctx context.Context, id domain.ScanID, items []domain.WorkItem) error {
	if len(items) == 0 {
		return nil
	}
	// Guard the parent scan's existence (mem does), so a fan-out/resume against a
	// nonexistent scan fails loudly instead of creating orphan ledger docs.
	exists, err := m.exists(ctx, m.scans, id)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("taskstore: scan %q not found", id)
	}
	docs := make([]interface{}, len(items))
	for i, it := range items {
		docs[i] = platform.WorkLedgerEntry{ID: it.ID, ScanID: id, Item: it, State: platform.ItemPending}
	}
	if _, err := m.ledger.InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("taskstore: AppendWorkItems %q: %w", id, err)
	}
	return nil
}

// CompareAndSetItemState is the single-winner claim (PR-4): the expected-state
// filter makes findOneAndUpdate atomic, so exactly one concurrent claimer swaps
// pending→inflight. Not-found vs lost-race are distinguished as in
// CompareAndSetState.
func (m *Mongo) CompareAndSetItemState(ctx context.Context, wid domain.WorkItemID, expected, next platform.ItemState) (bool, error) {
	set := bson.M{"state": next}
	if next != platform.ItemInflight {
		// Leaving inflight (sweep reclaim reset to pending, etc.) clears the lease.
		set["leaseOwner"], set["leaseExpiry"] = "", 0
	}
	res := m.ledger.FindOneAndUpdate(ctx,
		bson.M{"_id": wid, "state": expected.String()},
		bson.M{"$set": set},
	)
	if errors.Is(res.Err(), mongo.ErrNoDocuments) {
		exists, err := m.exists(ctx, m.ledger, wid)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("taskstore: work item %q not found", wid)
		}
		return false, nil // exists but wrong expected state → lost race
	}
	if res.Err() != nil {
		return false, fmt.Errorf("taskstore: CAS item %q: %w", wid, res.Err())
	}
	return true, nil
}

// exists reports whether a document with the given _id is present.
func (m *Mongo) exists(ctx context.Context, coll *mongo.Collection, id any) (bool, error) {
	err := coll.FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("taskstore: exists check: %w", err)
	}
	return true, nil
}

func (m *Mongo) ListScansByState(ctx context.Context, state platform.ScanState) ([]domain.ScanID, error) {
	cur, err := m.scans.Find(ctx, bson.M{"state": state.String()}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, fmt.Errorf("taskstore: ListScansByState: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []domain.ScanID
	for cur.Next(ctx) {
		var doc struct {
			ID domain.ScanID `bson:"_id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("taskstore: ListScansByState decode: %w", err)
		}
		out = append(out, doc.ID)
	}
	return out, cur.Err()
}

// ClaimItem atomically claims a pending item OR steals an expired-lease inflight
// item (leaseExpiry < now), stamping the new lease (BR-L1/L4). The $or filter +
// findOneAndUpdate makes the steal atomic, so a live worker's item (whose
// heartbeat pushed leaseExpiry into the future) never matches and is never stolen.
func (m *Mongo) ClaimItem(ctx context.Context, wid domain.WorkItemID, lease platform.Lease, now domain.Timestamp) (bool, error) {
	res := m.ledger.FindOneAndUpdate(ctx,
		bson.M{"_id": wid, "$or": bson.A{
			bson.M{"state": platform.ItemPending.String()},
			bson.M{"state": platform.ItemInflight.String(), "leaseExpiry": bson.M{"$lt": now}},
		}},
		bson.M{"$set": bson.M{
			"state":       platform.ItemInflight.String(),
			"leaseOwner":  lease.Owner,
			"leaseExpiry": lease.Expiry,
		}},
	)
	if errors.Is(res.Err(), mongo.ErrNoDocuments) {
		// not claimable (live inflight / done) OR absent — match the mem contract:
		// a genuinely-missing item is an error, a lost race is (false, nil).
		exists, err := m.exists(ctx, m.ledger, wid)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("taskstore: work item %q not found", wid)
		}
		return false, nil
	}
	if res.Err() != nil {
		return false, fmt.Errorf("taskstore: ClaimItem %q: %w", wid, res.Err())
	}
	return true, nil
}

// CountUnfinished returns |pending ∪ inflight| via an indexed count (O(log N)).
func (m *Mongo) CountUnfinished(ctx context.Context, id domain.ScanID) (int, error) {
	n, err := m.ledger.CountDocuments(ctx, bson.M{"scanId": id, "state": bson.M{"$in": bson.A{
		platform.ItemPending.String(), platform.ItemInflight.String(),
	}}})
	if err != nil {
		return 0, fmt.Errorf("taskstore: CountUnfinished: %w", err)
	}
	return int(n), nil
}

// RefreshLease extends the lease only while still inflight and owned (BR-L2).
func (m *Mongo) RefreshLease(ctx context.Context, wid domain.WorkItemID, owner string, newExpiry domain.Timestamp) (bool, error) {
	res := m.ledger.FindOneAndUpdate(ctx,
		bson.M{"_id": wid, "state": platform.ItemInflight.String(), "leaseOwner": owner},
		bson.M{"$set": bson.M{"leaseExpiry": newExpiry}},
	)
	if errors.Is(res.Err(), mongo.ErrNoDocuments) {
		return false, nil // reclaimed/stolen
	}
	if res.Err() != nil {
		return false, fmt.Errorf("taskstore: RefreshLease %q: %w", wid, res.Err())
	}
	return true, nil
}

func (m *Mongo) ListExpiredInflight(ctx context.Context, id domain.ScanID, now domain.Timestamp) ([]domain.WorkItem, error) {
	filter := bson.M{"scanId": id, "state": platform.ItemInflight.String(), "leaseExpiry": bson.M{"$lt": now}}
	return m.findItems(ctx, filter, "ListExpiredInflight")
}

// RecordOutcome finalizes only if still inflight and owned by `owner` (BR-L5).
//
// The outcome carries the item's findings, evidence and all — the ledger is how a
// worker's results reach the manager. That makes this collection a durable home
// for response fragments, so the evidence is sealed on the way in (data-census
// D1/O-4); without this, encrypting the findings collection alone would leave the
// same plaintext sitting in the ledger next to it.
func (m *Mongo) RecordOutcome(ctx context.Context, wid domain.WorkItemID, owner string, o domain.WorkItemOutcome) (bool, error) {
	sealed, err := m.sealOutcome(o)
	if err != nil {
		return false, err
	}
	res := m.ledger.FindOneAndUpdate(ctx,
		bson.M{"_id": wid, "state": platform.ItemInflight.String(), "leaseOwner": owner},
		bson.M{"$set": bson.M{"state": platform.ItemDone.String(), "outcome": sealed, "leaseOwner": "", "leaseExpiry": 0}},
	)
	if errors.Is(res.Err(), mongo.ErrNoDocuments) {
		// reclaimed/stolen (reject) vs genuinely-absent (error) — match mem.
		exists, err := m.exists(ctx, m.ledger, wid)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("taskstore: work item %q not found", wid)
		}
		return false, nil
	}
	if res.Err() != nil {
		return false, fmt.Errorf("taskstore: RecordOutcome %q: %w", wid, res.Err())
	}
	return true, nil
}

func (m *Mongo) ListUnfinished(ctx context.Context, id domain.ScanID) ([]domain.WorkItem, error) {
	filter := bson.M{"scanId": id, "state": bson.M{"$in": bson.A{
		platform.ItemPending.String(), platform.ItemInflight.String(),
	}}}
	return m.findItems(ctx, filter, "ListUnfinished")
}

func (m *Mongo) AllItems(ctx context.Context, id domain.ScanID) ([]domain.WorkItem, error) {
	return m.findItems(ctx, bson.M{"scanId": id}, "AllItems")
}

func (m *Mongo) findItems(ctx context.Context, filter bson.M, who string) ([]domain.WorkItem, error) {
	cur, err := m.ledger.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("taskstore: %s: %w", who, err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []domain.WorkItem
	for cur.Next(ctx) {
		var e ledgerDoc
		if err := cur.Decode(&e); err != nil {
			return nil, fmt.Errorf("taskstore: %s decode: %w", who, err)
		}
		out = append(out, e.Item)
	}
	return out, cur.Err()
}

func (m *Mongo) AllOutcomes(ctx context.Context, id domain.ScanID) ([]domain.WorkItemOutcome, error) {
	cur, err := m.ledger.Find(ctx, bson.M{"scanId": id, "outcome": bson.M{"$ne": nil}})
	if err != nil {
		return nil, fmt.Errorf("taskstore: AllOutcomes: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []domain.WorkItemOutcome
	for cur.Next(ctx) {
		var e ledgerDoc
		if err := cur.Decode(&e); err != nil {
			return nil, fmt.Errorf("taskstore: AllOutcomes decode: %w", err)
		}
		if e.Outcome == nil {
			continue
		}
		o, err := m.openOutcome(*e.Outcome)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, cur.Err()
}

// PurgeOutcomeEvidence drops the evidence the ledger is still carrying for a scan
// (G1'=E). Once the findings have been written to their own collection, the
// ledger's copy is dead weight: nothing reads it again, and resume only ever
// replays items that never finished. Removing it collapses the number of places a
// scan's evidence exists from two down to one (data-census rule O-2 — a transport
// copy dies when the transport is done).
//
// It leaves the rest of the outcome (the item id, which checkers failed, which
// were not applicable) in place: that is what the ledger is actually for, and it
// is not sensitive.
//
// The caller must only invoke this AFTER the findings are durably stored.
func (m *Mongo) PurgeOutcomeEvidence(ctx context.Context, id domain.ScanID) error {
	_, err := m.ledger.UpdateMany(ctx,
		bson.M{"scanId": id, "outcome": bson.M{"$ne": nil}},
		bson.M{"$unset": bson.M{"outcome.findings.$[].evidence": ""}},
	)
	if err != nil {
		return fmt.Errorf("taskstore: PurgeOutcomeEvidence %q: %w", id, err)
	}
	return nil
}

// sealOutcome encrypts the evidence of every finding an outcome carries, binding
// each envelope to the work item and the finding it belongs to so a sealed value
// cannot be transplanted onto another.
func (m *Mongo) sealOutcome(o domain.WorkItemOutcome) (sealedOutcome, error) {
	out := sealedOutcome{
		ID:            o.ID,
		Failed:        o.Failed,
		NotApplicable: o.NotApplicable,
		Findings:      make([]sealedFinding, 0, len(o.Findings)),
	}
	for _, f := range o.Findings {
		sf := sealedFinding{
			CWEs:       f.CWEs,
			TargetURL:  f.TargetURL,
			SourceTool: f.SourceTool,
			Severity:   f.Severity,
		}
		if len(f.Evidence.Snippets) > 0 {
			pt, err := json.Marshal(f.Evidence.Snippets)
			if err != nil {
				return sealedOutcome{}, fmt.Errorf("taskstore: marshal evidence: %w", err)
			}
			env, err := crypto.Seal(m.kr, pt, crypto.LedgerEvidenceAAD(string(o.ID), string(f.Key())))
			if err != nil {
				return sealedOutcome{}, fmt.Errorf("taskstore: seal evidence: %w", err)
			}
			sf.Evidence = env
		}
		out.Findings = append(out.Findings, sf)
	}
	return out, nil
}

func (m *Mongo) openOutcome(s sealedOutcome) (domain.WorkItemOutcome, error) {
	o := domain.WorkItemOutcome{
		ID:            s.ID,
		Failed:        s.Failed,
		NotApplicable: s.NotApplicable,
		Findings:      make([]domain.Finding, 0, len(s.Findings)),
	}
	for _, sf := range s.Findings {
		f := domain.Finding{
			CWEs:       sf.CWEs,
			TargetURL:  sf.TargetURL,
			SourceTool: sf.SourceTool,
			Severity:   sf.Severity,
		}
		// An empty envelope means either "this finding had no evidence" or "the
		// evidence was purged after the scan finalized". Both are legitimate, and
		// neither is a decryption failure.
		if !sf.Evidence.Empty() {
			pt, err := crypto.Open(m.kr, sf.Evidence, crypto.LedgerEvidenceAAD(string(s.ID), string(f.Key())))
			if err != nil {
				return domain.WorkItemOutcome{}, fmt.Errorf("taskstore: open evidence on item %q: %w", s.ID, err)
			}
			if err := json.Unmarshal(pt, &f.Evidence.Snippets); err != nil {
				return domain.WorkItemOutcome{}, fmt.Errorf("taskstore: unmarshal evidence on item %q: %w", s.ID, err)
			}
		}
		o.Findings = append(o.Findings, f)
	}
	return o, nil
}

// SaveReport stores the report's SKELETON — coverage, framework labels, mapping
// revision — and deliberately not its findings, which belong to the FindingsStore
// (data-census O-1). Embedding them here would give every finding a second durable
// home, with its own copy of the evidence to encrypt and expire, and would put the
// whole report back under the 16MB document ceiling that motivated splitting them
// out in the first place.
//
// This is a storage decision, not a wire one: the report served over the API still
// carries its findings. The two are joined on read, in exactly one place
// (Orchestrator.GetReport).
func (m *Mongo) SaveReport(ctx context.Context, id domain.ScanID, r report.Report) error {
	skeleton := r
	skeleton.Findings = nil
	_, err := m.reports.ReplaceOne(ctx, bson.M{"_id": id}, reportDoc{ID: id, Report: skeleton}, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("taskstore: SaveReport %q: %w", id, err)
	}
	return nil
}

func (m *Mongo) LoadReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error) {
	var doc reportDoc
	err := m.reports.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return report.Report{}, false, nil
	}
	if err != nil {
		return report.Report{}, false, fmt.Errorf("taskstore: LoadReport %q: %w", id, err)
	}
	return doc.Report, true, nil
}
