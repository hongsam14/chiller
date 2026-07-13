// workqueue_valkey.go is the durable WorkQueue (Q7=B/Q8=A/C2=A), added beside
// the in-process Mem behind the identical platform.WorkQueue interface (PR-11).
// It uses a single shared Redis/Valkey Stream + consumer group `workers` so a
// resident, autoscaled worker pool (Q2=B) competes for items — the canonical
// consumer-group model.
//
// The 3-method seam has no Ack; C2=A keeps it that way by encapsulating the
// XACK/recovery INSIDE the impl:
//   - an internal reader goroutine XREADGROUPs new entries and hands each to a
//     Dequeue caller, then XACKs it (an item leaving the queue = a worker took it);
//   - a reaper goroutine XAUTOCLAIMs entries a crashed process left un-XACKed and
//     redelivers them;
//   - correctness (no double-execution) is guaranteed regardless of redelivery by
//     the ledger CAS claim (PR-4, E-6 rule 5), and stuck-inflight items are
//     recovered by ResumeScan+ListUnfinished — so the queue only needs
//     at-least-once delivery.
//
// Drain is lazy: it marks the scan drained; the reader skips+XACKs its items so
// they are never dispatched (a shared stream cannot be DEL'd per scan).
package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/redis/go-redis/v9"
)

const (
	defaultStream     = "chiller:queue:stream"
	defaultGroup      = "workers"
	defaultDrainedKey = "chiller:queue:drained"
)

// Valkey is the durable WorkQueue. Construct with NewValkey.
type Valkey struct {
	rdb        *redis.Client
	stream     string
	group      string
	consumer   string
	drainedKey string

	ch          chan domain.WorkItem
	maxLen      int64
	blockDur    time.Duration
	reaperEvery time.Duration
	minIdle     time.Duration

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	reclaims atomic.Int64
}

var _ platform.WorkQueue = (*Valkey)(nil)
var _ platform.QueueInspector = (*Valkey)(nil)

// Stats reports live queue introspection for the dashboard (QueueInspector). It
// surfaces Redis errors rather than masking an outage as a healthy-empty queue:
// any failed read returns a non-nil error so the endpoint answers 500, not a
// misleading zeroed snapshot. Depth is the consumer-group LAG (undelivered
// backlog) — NOT XLEN, which counts every entry ever added including acked ones.
func (v *Valkey) Stats(ctx context.Context) (platform.QueueStats, error) {
	out := platform.QueueStats{Reclaimed: v.reclaims.Load()}

	groups, err := v.rdb.XInfoGroups(ctx, v.stream).Result()
	if err != nil {
		return platform.QueueStats{}, fmt.Errorf("workqueue: stats groups: %w", err)
	}
	for _, g := range groups {
		if g.Name == v.group {
			out.Depth = int(g.Lag) // undelivered backlog
			break
		}
	}

	d, err := v.rdb.SCard(ctx, v.drainedKey).Result()
	if err != nil {
		return platform.QueueStats{}, fmt.Errorf("workqueue: stats drained: %w", err)
	}
	out.Drained = int(d)

	p, err := v.rdb.XPending(ctx, v.stream, v.group).Result()
	if err != nil {
		return platform.QueueStats{}, fmt.Errorf("workqueue: stats pending: %w", err)
	}
	if p != nil {
		out.Pending = int(p.Count)
		for name, cnt := range p.Consumers {
			out.Consumers = append(out.Consumers, platform.ConsumerStat{Name: name, Pending: int(cnt)})
		}
	}
	return out, nil
}

// Options configure the Valkey queue. Zero values fall back to sensible defaults.
type Options struct {
	Password string
	DB       int
	TLS      bool // set true to require TLS (SECURITY-01); the caller supplies a *tls.Config via TLSConfig
	// Consume starts the reader+reaper goroutines so this process pulls work.
	// Producer-only roles (the manager, which only Enqueues) MUST leave it false
	// so they do not steal items off the shared stream that they cannot dispatch.
	Consume     bool
	Consumer    string
	Stream      string
	Group       string
	MaxLen      int64 // approximate stream cap (backstop against unbounded growth); 0 → default
	BlockDur    time.Duration
	ReaperEvery time.Duration
	MinIdle     time.Duration
}

// defaultMaxLen bounds the stream so acked-but-not-trimmed history cannot grow
// without limit. Approximate (~) so trimming is cheap; sized well above the live
// backlog a healthy queue holds, so it never trims undelivered work.
const defaultMaxLen = 1_000_000

// NewValkey connects to Valkey, ensures the consumer group exists, verifies
// connectivity (fail-closed, SECURITY-15), and starts the reader + reaper
// goroutines. addr is host:port.
func NewValkey(ctx context.Context, addr, consumer string, opts Options) (*Valkey, error) {
	stream := orDefault(opts.Stream, defaultStream)
	group := orDefault(opts.Group, defaultGroup)
	if consumer == "" {
		consumer = orDefault(opts.Consumer, "worker")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: opts.Password, DB: opts.DB})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("workqueue: valkey ping (fail-closed): %w", err)
	}
	// MKSTREAM creates the stream if absent; BUSYGROUP means it already exists.
	if err := rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err(); err != nil &&
		!strings.Contains(err.Error(), "BUSYGROUP") {
		_ = rdb.Close()
		return nil, fmt.Errorf("workqueue: create group: %w", err)
	}
	v := &Valkey{
		rdb:         rdb,
		stream:      stream,
		group:       group,
		consumer:    consumer,
		drainedKey:  defaultDrainedKey,
		ch:          make(chan domain.WorkItem),
		maxLen:      orInt64(opts.MaxLen, defaultMaxLen),
		blockDur:    orDuration(opts.BlockDur, 5*time.Second),
		reaperEvery: orDuration(opts.ReaperEvery, 30*time.Second),
		minIdle:     orDuration(opts.MinIdle, 60*time.Second),
	}
	runCtx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	// Only a consuming role (worker/all) runs the reader+reaper. A producer-only
	// manager leaves them off so it never claims items it cannot dispatch.
	if opts.Consume {
		v.wg.Add(2)
		go v.readLoop(runCtx)
		go v.reaperLoop(runCtx)
	}
	return v, nil
}

func (v *Valkey) Enqueue(ctx context.Context, item domain.WorkItem) error {
	// Match the mem contract: a drained scan's enqueues are dropped.
	drained, err := v.rdb.SIsMember(ctx, v.drainedKey, string(item.ScanID)).Result()
	if err != nil {
		return fmt.Errorf("workqueue: drained check: %w", err)
	}
	if drained {
		return nil
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("workqueue: marshal item: %w", err)
	}
	if err := v.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: v.stream,
		// Approximate MAXLEN backstops unbounded growth: XACK clears the PEL but
		// never the stream, so without this the stream would accumulate every
		// entry ever enqueued and eventually exhaust Valkey memory.
		MaxLen: v.maxLen,
		Approx: true,
		Values: map[string]interface{}{"data": payload, "scanId": string(item.ScanID)},
	}).Err(); err != nil {
		return fmt.Errorf("workqueue: xadd: %w", err)
	}
	return nil
}

// Dequeue hands a worker the next item, skipping any whose scan has been drained.
//
// The reader already drops drained items on its way into the channel, but that is
// not enough: it reads AHEAD, so an item can be sitting in the channel before its
// scan is drained and would then be handed out anyway. StopScan drains and expects
// the queue to stop dispatching, and the in-memory queue checks at exactly this
// point — so the durable one has to as well, or "stopped" means something different
// depending on which backend is wired.
//
// The item is already XACKed by the reader, so skipping it here simply drops it.
func (v *Valkey) Dequeue(ctx context.Context) (domain.WorkItem, error) {
	for {
		select {
		case it := <-v.ch:
			// DELIBERATE REGRESSION (adversarial test of the CI pipeline):
			// the read-ahead recheck is removed, reintroducing the Drain bug that
			// made StopScan not actually stop a scan. This branch must go RED.
			return it, nil
		case <-ctx.Done():
			return domain.WorkItem{}, ctx.Err()
		}
	}
}

func (v *Valkey) Drain(ctx context.Context, id domain.ScanID) error {
	if err := v.rdb.SAdd(ctx, v.drainedKey, string(id)).Err(); err != nil {
		return fmt.Errorf("workqueue: drain %q: %w", id, err)
	}
	return nil
}

// Ping verifies connectivity for the deep health check (RESILIENCY-06).
func (v *Valkey) Ping(ctx context.Context) error { return v.rdb.Ping(ctx).Err() }

// Close stops the background goroutines and closes the client. Not part of the
// seam; called by the composition root on shutdown.
func (v *Valkey) Close() error {
	v.cancel()
	v.wg.Wait()
	return v.rdb.Close()
}

// readLoop pulls never-delivered entries and hands each to a Dequeue caller.
func (v *Valkey) readLoop(ctx context.Context) {
	defer v.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := v.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    v.group,
			Consumer: v.consumer,
			Streams:  []string{v.stream, ">"},
			Count:    1,
			Block:    v.blockDur,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				continue // block timeout or shutdown
			}
			// transient error: back off briefly, then retry
			if !sleepCtx(ctx, 200*time.Millisecond) {
				return
			}
			continue
		}
		for _, st := range res {
			for _, msg := range st.Messages {
				v.deliver(ctx, msg)
			}
		}
	}
}

// reaperLoop reclaims entries a crashed process left un-XACKed and redelivers
// them (C2=A). Redelivery is safe: the ledger CAS rejects an already-claimed item.
func (v *Valkey) reaperLoop(ctx context.Context) {
	defer v.wg.Done()
	for {
		if !sleepCtx(ctx, v.reaperEvery) {
			return
		}
		msgs, _, err := v.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   v.stream,
			Group:    v.group,
			Consumer: v.consumer,
			MinIdle:  v.minIdle,
			Start:    "0",
			Count:    32,
		}).Result()
		if err != nil {
			continue // best-effort; the ledger/ResumeScan path is the backstop
		}
		for _, msg := range msgs {
			v.reclaims.Add(1)
			v.deliver(ctx, msg)
		}
	}
}

// deliver decodes a stream entry, skips (and acks) drained scans, otherwise hands
// the item to a Dequeue caller and then acks it (item left the queue = a worker
// took it). Crash recovery after this ack is the ledger's job (ResumeScan).
func (v *Valkey) deliver(ctx context.Context, msg redis.XMessage) {
	scanID, _ := msg.Values["scanId"].(string)
	if scanID != "" {
		if drained, _ := v.rdb.SIsMember(ctx, v.drainedKey, scanID).Result(); drained {
			v.rdb.XAck(ctx, v.stream, v.group, msg.ID)
			return
		}
	}
	raw, _ := msg.Values["data"].(string)
	var item domain.WorkItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		// undecodable payload: ack it away so it does not loop forever.
		v.rdb.XAck(ctx, v.stream, v.group, msg.ID)
		return
	}
	for {
		select {
		case v.ch <- item:
			v.rdb.XAck(ctx, v.stream, v.group, msg.ID)
			return
		case <-ctx.Done():
			// shutting down before handoff: leave it in the PEL for the reaper.
			return
		case <-time.After(250 * time.Millisecond):
			// While blocked waiting for a free worker, re-check drained so a scan
			// stopped mid-wait has its item dropped instead of dispatched (narrows
			// the window vs mem's dispatch-time check; StopScan's ctx cancel is the
			// backstop that aborts anything already handed off).
			if scanID != "" {
				if drained, _ := v.rdb.SIsMember(ctx, v.drainedKey, scanID).Result(); drained {
					v.rdb.XAck(ctx, v.stream, v.group, msg.ID)
					return
				}
			}
		}
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDuration(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

func orInt64(n, def int64) int64 {
	if n <= 0 {
		return def
	}
	return n
}

// sleepCtx sleeps for d or until ctx is done; returns false if ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
