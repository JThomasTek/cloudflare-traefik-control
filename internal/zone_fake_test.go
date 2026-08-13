package internal

import (
	"context"
	"errors"
)

// errStub is a sentinel error reused by tests that make the zone fail.
var errStub = errors.New("stub zone error")

// fakeZone is the in-memory Zone adapter. It records what the reconciler asked
// for so tests can assert on the calls, and holds the resulting records so they
// can assert on the outcome. Ownership comments are deliberately not modelled —
// they live behind the seam, in ownership.go and the Cloudflare adapter.
type fakeZone struct {
	records map[string]fakeRecord

	// Injected failures, applied per method.
	addErr    error
	setIPErr  error
	removeErr error

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
	z.adds = append(z.adds, router)

	if z.addErr != nil {
		return z.addErr
	}

	z.records[router] = fakeRecord{host: host, ip: ip}

	return nil
}

func (z *fakeZone) SetIP(ctx context.Context, ip string) error {
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
