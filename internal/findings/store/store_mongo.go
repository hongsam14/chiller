// store_mongo.go is the durable FindingsStore, added beside InMemory behind the
// identical FindingsStore interface: the orchestrator never learns which one it
// got.
//
// Findings get their own collection rather than riding along inside the stored
// report (Q1=A). Three things follow from that, and each is the reason for a piece
// of this file:
//
//   - One document per finding, so a scan with a lot of findings cannot blow the
//     16MB BSON document limit that an embedded array would eventually hit.
//   - The document _id is scanId+idx, so re-saving a scan overwrites in place
//     (idempotent) rather than accumulating duplicates — and a save that produced
//     FEWER findings than a previous one deletes the tail, or the scan would keep
//     phantom findings from the earlier run.
//   - The load sorts by idx, because Mongo returns documents in no particular
//     order and the FindingsStore contract promises the caller's original order
//     (a report's framework labels are resolved against that slice).
//
// Evidence is sealed before it is written (SECURITY-01): the response fragments a
// finding carries can contain tokens or PII, and this collection is the one place
// they live for the long term. Everything else on the document stays in the clear,
// because it is what we query and index on.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hongsam14/chiller/internal/crypto"
	"github.com/hongsam14/chiller/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

// schemaVersion marks the document layout so a future migration has an explicit
// branch point.
const schemaVersion = 1

// idSep separates the parts of a finding document's _id. Unit-separator (0x1f)
// cannot appear in a scan id.
const idSep = "\x1f"

// Mongo is the durable FindingsStore. Construct with NewMongo.
type Mongo struct {
	client   *mongo.Client
	findings *mongo.Collection
	kr       crypto.Keyring
	ttl      time.Duration
}

var _ FindingsStore = (*Mongo)(nil)

// findingDoc is one finding as persisted. The queryable fields (scan, ordering,
// identity, CWE, URL, tool, severity) are plaintext; only the evidence is sealed.
type findingDoc struct {
	ID            string            `bson:"_id"`
	ScanID        domain.ScanID     `bson:"scanId"`
	Idx           int               `bson:"idx"`
	Key           domain.FindingKey `bson:"key"`
	CWEs          []domain.CWE      `bson:"cwes"`
	TargetURL     string            `bson:"targetUrl"`
	SourceTool    string            `bson:"sourceTool"`
	Severity      domain.Severity   `bson:"severity"`
	Evidence      crypto.Envelope   `bson:"evidence"`
	SchemaVersion int               `bson:"schemaVersion"`
	CreatedAt     time.Time         `bson:"createdAt"`
}

// NewMongo connects to Mongo (TLS enforced via the URI, SECURITY-01) with majority
// write-concern, verifies connectivity (fail-closed, SECURITY-15), and ensures the
// indexes. It shares the instance with the TaskStore and separates by collection
// (shared-infrastructure Q9=C).
//
// ttl > 0 installs a TTL index so findings expire; ttl == 0 (the default) keeps
// them indefinitely (Q7=C). NOTE for operators: turning the TTL on expires findings
// but NOT the reports that reference them, so set the same TTL on the reports
// collection or you will be left with reports whose findings have vanished.
func NewMongo(ctx context.Context, uri, dbName string, kr crypto.Keyring, ttl time.Duration) (*Mongo, error) {
	if kr == nil {
		return nil, errors.New("store: mongo needs a keyring (evidence is encrypted at rest)")
	}
	// A TTL under a second would round to expireAfterSeconds: 0, which Mongo reads as
	// "expire at createdAt" — every finding deleted within a minute of the scan
	// finishing. Refuse it rather than silently shredding the results.
	if ttl > 0 && ttl < time.Minute {
		return nil, fmt.Errorf("store: findings TTL %v is too short (minimum 1m); 0 keeps findings indefinitely", ttl)
	}
	if dbName == "" {
		dbName = "chiller"
	}
	opts := options.Client().ApplyURI(uri).SetWriteConcern(writeconcern.Majority())
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("store: mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: mongo ping (fail-closed): %w", err)
	}
	m := &Mongo{
		client:   client,
		findings: client.Database(dbName).Collection("findings"),
		kr:       kr,
		ttl:      ttl,
	}
	if err := m.ensureIndexes(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return m, nil
}

// ttlIndexName is fixed so the TTL index can be found again and reconciled when
// the retention flag changes.
const ttlIndexName = "findings_ttl"

func (m *Mongo) ensureIndexes(ctx context.Context) error {
	models := []mongo.IndexModel{
		// Serves both the LoadFindings query and its sort.
		{Keys: bson.D{{Key: "scanId", Value: 1}, {Key: "idx", Value: 1}}},
		// Key lookup for report consumers.
		{Keys: bson.D{{Key: "scanId", Value: 1}, {Key: "key", Value: 1}}},
	}
	if _, err := m.findings.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("store: mongo ensure indexes: %w", err)
	}
	return m.ensureTTLIndex(ctx)
}

// ensureTTLIndex reconciles the TTL index with the CURRENT retention setting.
//
// Creating it unconditionally does not work: Mongo rejects a CreateIndex that
// names an existing index with a different expireAfterSeconds (IndexOptionsConflict),
// so simply changing --findings-ttl and redeploying would crash-loop every pod. And
// turning the flag back off has to actually remove the index, or findings would keep
// expiring on a setting nobody set any more.
func (m *Mongo) ensureTTLIndex(ctx context.Context) error {
	if m.ttl <= 0 {
		// Retention is indefinite (the default): make sure a TTL index left over from
		// an earlier configuration is gone.
		_, err := m.findings.Indexes().DropOne(ctx, ttlIndexName)
		if err != nil && !isIndexNotFound(err) {
			return fmt.Errorf("store: mongo drop stale TTL index: %w", err)
		}
		return nil
	}

	model := mongo.IndexModel{
		Keys: bson.D{{Key: "createdAt", Value: 1}},
		Options: options.Index().
			SetName(ttlIndexName).
			SetExpireAfterSeconds(int32(m.ttl.Round(time.Second).Seconds())),
	}
	_, err := m.findings.Indexes().CreateOne(ctx, model)
	if err == nil {
		return nil
	}
	// The TTL changed. Mongo will not amend it in place through CreateIndex, so drop
	// and rebuild — the index is derived state, there is nothing to lose.
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Code == indexOptionsConflict {
		if _, derr := m.findings.Indexes().DropOne(ctx, ttlIndexName); derr != nil && !isIndexNotFound(derr) {
			return fmt.Errorf("store: mongo drop TTL index for rebuild: %w", derr)
		}
		if _, cerr := m.findings.Indexes().CreateOne(ctx, model); cerr != nil {
			return fmt.Errorf("store: mongo rebuild TTL index: %w", cerr)
		}
		return nil
	}
	return fmt.Errorf("store: mongo ensure TTL index: %w", err)
}

// indexOptionsConflict is Mongo's error code for "this index exists with different
// options".
const indexOptionsConflict = 85

func isIndexNotFound(err error) bool {
	var cmdErr mongo.CommandError
	return errors.As(err, &cmdErr) && cmdErr.Code == 27 // IndexNotFound
}

// Ping reports backend reachability for the deep health check.
func (m *Mongo) Ping(ctx context.Context) error { return m.client.Ping(ctx, nil) }

// Close disconnects the client.
func (m *Mongo) Close(ctx context.Context) error { return m.client.Disconnect(ctx) }

func docID(scanID domain.ScanID, idx int) string {
	return fmt.Sprintf("%s%s%d", scanID, idSep, idx)
}

func (m *Mongo) SaveFindings(ctx context.Context, scanID domain.ScanID, findings []domain.Finding) error {
	now := time.Now().UTC()
	models := make([]mongo.WriteModel, 0, len(findings))
	for i, f := range findings {
		key := f.Key()
		sealed, err := m.sealEvidence(scanID, key, f.Evidence)
		if err != nil {
			return err
		}
		doc := findingDoc{
			ID:            docID(scanID, i),
			ScanID:        scanID,
			Idx:           i,
			Key:           key,
			CWEs:          domain.NormalizeCWEs(f.CWEs),
			TargetURL:     f.TargetURL,
			SourceTool:    f.SourceTool,
			Severity:      f.Severity,
			Evidence:      sealed,
			SchemaVersion: schemaVersion,
			CreatedAt:     now,
		}
		// A finding with no evidence is written WITHOUT touching the evidence field
		// of an existing document, instead of replacing it wholesale.
		//
		// The reason is subtle and it cost us a data-loss bug. The ledger carries a
		// scan's evidence until finalize purges it. If a finalize ever reads the
		// ledger AFTER that purge — a re-trigger, a second finalizer in another pod —
		// it produces exactly the same findings but with empty evidence, and a plain
		// upsert would then overwrite the sealed evidence we already stored with
		// nothing. A save can therefore ADD evidence but never erase it: this store is
		// the last copy, and "no evidence" is never new information about a finding
		// that already has some.
		if sealed.Empty() {
			models = append(models, mongo.NewUpdateOneModel().
				SetFilter(bson.M{"_id": doc.ID}).
				SetUpdate(bson.M{
					"$set": bson.M{
						"scanId": doc.ScanID, "idx": doc.Idx, "key": doc.Key,
						"cwes": doc.CWEs, "targetUrl": doc.TargetURL,
						"sourceTool": doc.SourceTool, "severity": doc.Severity,
						"schemaVersion": doc.SchemaVersion, "createdAt": doc.CreatedAt,
					},
				}).
				SetUpsert(true))
			continue
		}
		models = append(models, mongo.NewReplaceOneModel().
			SetFilter(bson.M{"_id": doc.ID}).
			SetReplacement(doc).
			SetUpsert(true))
	}
	if len(models) > 0 {
		if _, err := m.findings.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("store: SaveFindings %q: %w", scanID, err)
		}
	}
	// Drop anything a longer previous save left behind, so the stored list is
	// exactly what we were given rather than a union with the last run.
	if _, err := m.findings.DeleteMany(ctx, bson.M{
		"scanId": scanID,
		"idx":    bson.M{"$gte": len(findings)},
	}); err != nil {
		return fmt.Errorf("store: SaveFindings %q: truncate tail: %w", scanID, err)
	}
	return nil
}

func (m *Mongo) LoadFindings(ctx context.Context, scanID domain.ScanID) ([]domain.Finding, error) {
	cur, err := m.findings.Find(ctx, bson.M{"scanId": scanID},
		options.Find().SetSort(bson.D{{Key: "idx", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("store: LoadFindings %q: %w", scanID, err)
	}
	defer func() { _ = cur.Close(ctx) }()

	out := []domain.Finding{}
	for cur.Next(ctx) {
		var doc findingDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("store: LoadFindings %q: decode: %w", scanID, err)
		}
		ev, err := m.openEvidence(scanID, doc.Key, doc.Evidence)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.Finding{
			CWEs:       doc.CWEs,
			TargetURL:  doc.TargetURL,
			SourceTool: doc.SourceTool,
			Severity:   doc.Severity,
			Evidence:   ev,
		})
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("store: LoadFindings %q: cursor: %w", scanID, err)
	}
	return out, nil
}

// sealEvidence encrypts a finding's snippets, binding the envelope to the scan and
// the finding it belongs to, so a sealed value cannot be moved onto a different
// finding. Evidence with no snippets seals to the zero Envelope: "no evidence" is
// a legitimate state and must not require a key to read back.
func (m *Mongo) sealEvidence(scanID domain.ScanID, key domain.FindingKey, ev domain.Evidence) (crypto.Envelope, error) {
	if len(ev.Snippets) == 0 {
		return crypto.Envelope{}, nil
	}
	pt, err := json.Marshal(ev.Snippets)
	if err != nil {
		return crypto.Envelope{}, fmt.Errorf("store: marshal evidence: %w", err)
	}
	env, err := crypto.Seal(m.kr, pt, crypto.EvidenceAAD(string(scanID), string(key)))
	if err != nil {
		return crypto.Envelope{}, fmt.Errorf("store: seal evidence: %w", err)
	}
	return env, nil
}

func (m *Mongo) openEvidence(scanID domain.ScanID, key domain.FindingKey, env crypto.Envelope) (domain.Evidence, error) {
	if env.Empty() {
		return domain.Evidence{}, nil
	}
	pt, err := crypto.Open(m.kr, env, crypto.EvidenceAAD(string(scanID), string(key)))
	if err != nil {
		return domain.Evidence{}, fmt.Errorf("store: open evidence for %q: %w", key, err)
	}
	var snippets []string
	if err := json.Unmarshal(pt, &snippets); err != nil {
		return domain.Evidence{}, fmt.Errorf("store: unmarshal evidence for %q: %w", key, err)
	}
	return domain.Evidence{Snippets: snippets}, nil
}
