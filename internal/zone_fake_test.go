package internal

import (
	"context"
	"errors"
	"sync"
)

// errStub is a sentinel error reused by tests that make the zone fail.
var errStub = errors.New("stub zone error")

// fakeZone is the in-memory Zone adapter. It records what the reconciler asked
// for so tests can assert on the calls, and holds the resulting records so they
// can assert on the outcome. Ownership comments are deliberately not modelled —
// they live behind the seam, in ownership.go and the Cloudflare adapter.
//
// It is safe for concurrent use, because the reconcile paths it stands behind
// run from more than one goroutine.
type fakeZone struct {
	mu      sync.Mutex
	records map[string]fakeRecord

	// Injected failures, applied per method.
	addErr    error
	setIPErr  error
	removeErr error

	// addHook, when set, runs on entry to Add before any lock is taken, so a
	// test can suspend a reconcile part-way through and drive another one past
	// it. Set it before starting any goroutines.
	addHook func(router string)

	// Calls received, in order.
	adds    []string
	setIPs  []string
	removes []string
}

type fakeRecord struct {
	host string
	ip   string
}

func newFakeZone() *fakeZone {
	return &fakeZone{records: make(map[string]fakeRecord)}
}

func (z *fakeZone) Add(ctx context.Context, router string, host string, ip string) error {
	if z.addHook != nil {
		z.addHook(router)
	}

	z.mu.Lock()
	defer z.mu.Unlock()

	z.adds = append(z.adds, router)

	if z.addErr != nil {
		return z.addErr
	}

	z.records[router] = fakeRecord{host: host, ip: ip}

	return nil
}

func (z *fakeZone) SetIP(ctx context.Context, ip string) error {
	z.mu.Lock()
	defer z.mu.Unlock()

	z.setIPs = append(z.setIPs, ip)

	if z.setIPErr != nil {
		return z.setIPErr
	}

	for router, record := range z.records {
		record.ip = ip
		z.records[router] = record
	}

	return nil
}

func (z *fakeZone) Remove(ctx context.Context, router string) error {
	z.mu.Lock()
	defer z.mu.Unlock()

	z.removes = append(z.removes, router)

	if z.removeErr != nil {
		return z.removeErr
	}

	// Idempotent, matching the interface contract: deleting an absent record
	// is not an error.
	delete(z.records, router)

	return nil
}

// fakeZone must satisfy the same interface the Cloudflare adapter does.
var _ Zone = (*fakeZone)(nil)
