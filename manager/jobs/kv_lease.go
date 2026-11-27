// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	// DefaultLeaseBucket is the KV bucket that holds the job leases.
	DefaultLeaseBucket = "maeve_job_leases"
	// DefaultLeaseTTL is how long a lease lasts after its last write.
	DefaultLeaseTTL = 15 * time.Second
	// minLeaseTTL is the shortest max age that a JetStream stream accepts.
	minLeaseTTL = 100 * time.Millisecond
)

// errNotConnected is returned instead of sending a request while the connection to NATS
// is down: nats.go would buffer the request and send it after the reconnect, long after
// the caller gave up on it.
var errNotConnected = errors.New("not connected to NATS")

var bucketNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// KVLeaseConfig configures the bucket that holds the leases.
type KVLeaseConfig struct {
	Bucket   string        // "maeve_job_leases"
	TTL      time.Duration // 15s: the max age of the bucket, so a lease that is not renewed expires
	Replicas int           // 1 dev, 3 prod (only used with Ensure; 0 means 1)
	Ensure   bool          // create/update the bucket instead of only looking it up
}

// KVLease is a Lease in a NATS KV bucket whose TTL is the lease TTL. The key of a job is
// "job.<name>". A lease is taken by creating the key, which only succeeds when the key
// is absent, deleted or expired; it is renewed and released with writes that only
// succeed while the key still has the revision of this holder's last write. A holder
// can therefore never extend or delete a lease that another holder took.
type KVLease struct {
	js       jetstream.JetStream
	cfg      KVLeaseConfig
	instance string

	bindMu sync.Mutex // one bind at a time

	mu   sync.Mutex
	kv   jetstream.KeyValue   // nil until the bucket is bound
	held map[string]heldLease // name → this holder's last write
}

// heldLease is the last write of a lease that this holder holds.
type heldLease struct {
	revision   uint64
	acquiredAt time.Time
}

// leaseValue is the value of a lease key, for whoever looks at the bucket.
type leaseValue struct {
	Holder     string    `json:"holder"`
	AcquiredAt time.Time `json:"acquired_at"`
	RenewedAt  time.Time `json:"renewed_at"`
}

// NewKVLease creates the lease and binds its bucket now: with cfg.Ensure the bucket is
// created or updated, otherwise it must exist. It fails when the configuration is not
// valid or the bucket cannot be bound.
func NewKVLease(ctx context.Context, js jetstream.JetStream, cfg KVLeaseConfig, instance string) (*KVLease, error) {
	l, err := NewUnboundKVLease(js, cfg, instance)
	if err != nil {
		return nil, err
	}
	if err := l.Bind(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

// NewUnboundKVLease creates the lease without contacting NATS: the bucket is bound by the
// first operation that needs it, and again after an operation found it missing. It fails
// only when the configuration is not valid.
func NewUnboundKVLease(js jetstream.JetStream, cfg KVLeaseConfig, instance string) (*KVLease, error) {
	if js == nil {
		return nil, errors.New("job lease: no jetstream context")
	}
	if !bucketNamePattern.MatchString(cfg.Bucket) {
		return nil, fmt.Errorf("job lease bucket: not a valid bucket name: %q", cfg.Bucket)
	}
	if cfg.TTL < minLeaseTTL {
		return nil, fmt.Errorf("job lease ttl must be at least %s: %s", minLeaseTTL, cfg.TTL)
	}
	if cfg.Replicas < 0 {
		return nil, fmt.Errorf("job lease bucket replicas must not be negative: %d", cfg.Replicas)
	}
	if instance == "" {
		return nil, errors.New("job lease: no instance id")
	}
	return &KVLease{js: js, cfg: cfg, instance: instance, held: make(map[string]heldLease)}, nil
}

// Instance is the holder id that this lease writes into the keys it holds.
func (l *KVLease) Instance() string {
	return l.instance
}

// Bind binds the bucket (creating or updating it with Ensure) unless it is bound already.
func (l *KVLease) Bind(ctx context.Context) error {
	_, err := l.bucket(ctx)
	return err
}

// Acquire creates the key of the job. A key that exists, also one written by this
// holder earlier, means the lease is held: false.
func (l *KVLease) Acquire(ctx context.Context, name string) (bool, error) {
	kv, err := l.bucket(ctx)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	value, err := json.Marshal(leaseValue{Holder: l.instance, AcquiredAt: now, RenewedAt: now})
	if err != nil {
		return false, fmt.Errorf("acquire job lease %s: %w", name, err)
	}
	if err := l.checkConnected(); err != nil {
		return false, fmt.Errorf("acquire job lease %s: %w", name, err)
	}
	rev, err := kv.Create(ctx, leaseKey(name), value)
	if errors.Is(err, jetstream.ErrKeyExists) {
		return false, nil
	}
	if err != nil {
		l.unbindIfMissing(kv, err)
		return false, fmt.Errorf("acquire job lease %s: %w", name, err)
	}

	l.mu.Lock()
	l.held[name] = heldLease{revision: rev, acquiredAt: now}
	l.mu.Unlock()
	return true, nil
}

// Renew writes the key again, only if it still has the revision of this holder's last
// write. Any failure forgets the lease: a revision that changed (the key expired, or
// another holder took it) gives ErrLeaseLost, other errors are returned as they are.
func (l *KVLease) Renew(ctx context.Context, name string) error {
	l.mu.Lock()
	h, ok := l.held[name]
	l.mu.Unlock()
	if !ok {
		return fmt.Errorf("renew job lease %s: %w (not held)", name, ErrLeaseLost)
	}

	rev, err := l.renew(ctx, name, h)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		delete(l.held, name)
		if errors.Is(err, jetstream.ErrKeyExists) {
			return fmt.Errorf("renew job lease %s: %w (%v)", name, ErrLeaseLost, err)
		}
		return fmt.Errorf("renew job lease %s: %w", name, err)
	}
	l.held[name] = heldLease{revision: rev, acquiredAt: h.acquiredAt}
	return nil
}

func (l *KVLease) renew(ctx context.Context, name string, h heldLease) (uint64, error) {
	kv, err := l.bucket(ctx)
	if err != nil {
		return 0, err
	}
	value, err := json.Marshal(leaseValue{Holder: l.instance, AcquiredAt: h.acquiredAt, RenewedAt: time.Now().UTC()})
	if err != nil {
		return 0, err
	}
	if err := l.checkConnected(); err != nil {
		return 0, err
	}
	rev, err := kv.Update(ctx, leaseKey(name), value, h.revision)
	if err != nil {
		l.unbindIfMissing(kv, err)
		return 0, err
	}
	return rev, nil
}

// Release deletes the key, only if it still has the revision of this holder's last
// write, and forgets the lease. A lease that is not held, or that another holder took
// meanwhile, is not an error.
func (l *KVLease) Release(ctx context.Context, name string) error {
	l.mu.Lock()
	h, ok := l.held[name]
	delete(l.held, name)
	l.mu.Unlock()
	if !ok {
		return nil
	}

	kv, err := l.bucket(ctx)
	if err != nil {
		return fmt.Errorf("release job lease %s: %w", name, err)
	}
	if err := l.checkConnected(); err != nil {
		return fmt.Errorf("release job lease %s: %w", name, err)
	}
	err = kv.Delete(ctx, leaseKey(name), jetstream.LastRevision(h.revision))
	if errors.Is(err, jetstream.ErrKeyExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("release job lease %s: %w", name, err)
	}
	return nil
}

// bucket returns the bound bucket, binding it first when needed.
func (l *KVLease) bucket(ctx context.Context) (jetstream.KeyValue, error) {
	if kv := l.boundBucket(); kv != nil {
		return kv, nil
	}

	l.bindMu.Lock()
	defer l.bindMu.Unlock()
	// bound by another caller while this one waited
	if kv := l.boundBucket(); kv != nil {
		return kv, nil
	}
	if err := l.checkConnected(); err != nil {
		return nil, fmt.Errorf("job lease bucket %s: %w", l.cfg.Bucket, err)
	}

	var kv jetstream.KeyValue
	var err error
	if l.cfg.Ensure {
		replicas := l.cfg.Replicas
		if replicas == 0 {
			replicas = 1
		}
		kv, err = l.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:   l.cfg.Bucket,
			TTL:      l.cfg.TTL,
			History:  1,
			Storage:  jetstream.FileStorage,
			Replicas: replicas,
		})
	} else {
		kv, err = l.js.KeyValue(ctx, l.cfg.Bucket)
		if err == nil {
			err = l.checkBucketTTL(ctx, kv)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("job lease bucket %s: %w", l.cfg.Bucket, err)
	}

	l.mu.Lock()
	l.kv = kv
	l.mu.Unlock()
	return kv, nil
}

// checkBucketTTL refuses a bucket that was provisioned with a shorter TTL than the
// lease TTL (a lease could expire while its holder still runs the job) or without a TTL
// (the lease of an instance that died would never expire).
func (l *KVLease) checkBucketTTL(ctx context.Context, kv jetstream.KeyValue) error {
	status, err := kv.Status(ctx)
	if err != nil {
		return err
	}
	if status.TTL() == 0 || status.TTL() < l.cfg.TTL {
		return fmt.Errorf("the bucket TTL %s must be at least the lease TTL %s", status.TTL(), l.cfg.TTL)
	}
	return nil
}

func (l *KVLease) boundBucket() jetstream.KeyValue {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.kv
}

// unbindIfMissing forgets the bucket after an error that says it is gone, so that the
// next operation binds it again (and with Ensure creates it again).
func (l *KVLease) unbindIfMissing(kv jetstream.KeyValue, err error) {
	if !errors.Is(err, jetstream.ErrNoStreamResponse) &&
		!errors.Is(err, jetstream.ErrBucketNotFound) &&
		!errors.Is(err, jetstream.ErrStreamNotFound) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.kv == kv {
		l.kv = nil
	}
}

func (l *KVLease) checkConnected() error {
	if !l.js.Conn().IsConnected() {
		return errNotConnected
	}
	return nil
}

func leaseKey(name string) string {
	return "job." + name
}

// NewInstanceId returns a holder id for this process: the host name, a dash and six
// random hex characters, so that two processes on one host differ.
func NewInstanceId() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "manager"
	}
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%06x", host, time.Now().UnixNano()&0xffffff)
	}
	return host + "-" + hex.EncodeToString(b[:])
}
