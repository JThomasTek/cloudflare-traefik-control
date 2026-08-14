package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// DefaultStateFile is where CTC keeps its state unless pointed elsewhere.
const DefaultStateFile = "/etc/ctc/state.yml"

type state struct {
	WanIP   string
	Routers map[string]Router
}

// Store is CTC's record of what it has already done: which routers it has
// created managed records for, and the WAN IP those records carry.
//
// Every method that changes something is a complete read-modify-write held
// under one lock, so the reconcile paths — which run from separate goroutines —
// cannot overwrite each other's changes with a copy read before those changes
// happened. Callers cannot hold state across a mutation, because the interface
// gives them no way to express it.
//
// The file on disk is the source of truth; nothing is cached between calls.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore prepares the state file for use, creating its directory and an empty
// file if they are missing. An unusable state location is a misconfiguration,
// so it fails here at startup rather than on every reconcile forever.
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating state directory %q: %w", dir, err)
	}

	// O_CREATE without O_TRUNC: an existing state file is left exactly as it is.
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening state file %q: %w", path, err)
	}

	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("closing state file %q: %w", path, err)
	}

	return &Store{path: path}, nil
}

// Snapshot returns the state as it currently stands. The result shares nothing
// with the Store, so it is the caller's to keep — but it is only a view of one
// moment, and anything else may have moved on by the time it is used.
func (s *Store) Snapshot() (state, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.read()
}

// RecordRouter notes that a managed record now exists for the given router.
func (s *Store) RecordRouter(name string, router Router) error {
	return s.update(func(st *state) {
		st.Routers[name] = router
	})
}

// ForgetRouter drops a router, once its managed record is gone.
func (s *Store) ForgetRouter(name string) error {
	return s.update(func(st *state) {
		delete(st.Routers, name)
	})
}

// SetWanIP records the address the managed records now point at.
func (s *Store) SetWanIP(ip string) error {
	return s.update(func(st *state) {
		st.WanIP = ip
	})
}

// update reads, applies fn, and persists the result without releasing the lock
// in between. This is the whole point of the type: the cycle is indivisible.
func (s *Store) update(fn func(*state)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.read()
	if err != nil {
		return err
	}

	fn(&st)

	return s.write(st)
}

// read loads the state file. The caller must hold the lock.
func (s *Store) read() (state, error) {
	log.Debug().Msg("Reading state file data")

	buffer, err := os.ReadFile(s.path)
	if err != nil {
		return state{}, fmt.Errorf("reading state file %q: %w", s.path, err)
	}

	var st state

	if err := yaml.Unmarshal(buffer, &st); err != nil {
		// Refuse to guess. Failing to understand our own bookkeeping is the one
		// situation where CTC must touch nothing at all: treating an unreadable
		// file as "nothing is managed" would re-add every record, and treating
		// it as "no routers exist" would delete every record.
		return state{}, fmt.Errorf("parsing state file %q: %w", s.path, err)
	}

	// An empty file is a legitimate fresh install, and unmarshals to no map at
	// all. Make it usable so callers never meet a nil map.
	if st.Routers == nil {
		st.Routers = make(map[string]Router)
	}

	return st, nil
}

// write persists the state by rendering it to a temporary file alongside the
// real one and renaming over the top. Rename is atomic within a directory, so a
// crash or a full disk leaves the previous file intact instead of a truncated
// one — which matters because a truncated file parses cleanly as "nothing is
// managed", and CTC would then re-add every record it already owns.
//
// The caller must hold the lock.
func (s *Store) write(st state) error {
	log.Debug().Msg("Writing to the state file")

	data, err := yaml.Marshal(st)
	if err != nil {
		return err
	}

	// Same directory as the target, so the rename stays within one filesystem.
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.yml")
	if err != nil {
		return err
	}

	// Nothing half-written is left behind on any failure below. Harmlessly a
	// no-op once the rename has succeeded.
	defer os.Remove(temp.Name())

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}

	if err := temp.Close(); err != nil {
		return err
	}

	return os.Rename(temp.Name(), s.path)
}
