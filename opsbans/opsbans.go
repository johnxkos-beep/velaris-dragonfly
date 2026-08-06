package opsbans

import (
	"encoding/json"
	"os"
	"sync"
)

// ---------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------

// OpSet tracks which XUIDs are server operators, persisted to ops.json.
type OpSet struct {
	mu   sync.RWMutex
	path string
	data map[string]bool
}

// LoadOps reads the op set from path. If the file doesn't exist, an empty
// set is returned.
func LoadOps(path string) (*OpSet, error) {
	s := &OpSet{path: path, data: map[string]bool{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

// IsOp reports whether the given XUID is a server operator.
func (s *OpSet) IsOp(xuid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[xuid]
}

// Empty reports whether there are no operators on file yet. Used to
// auto-op the first player who ever joins, so there's always at least one
// way to grant op in-game.
func (s *OpSet) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data) == 0
}

// SetOp adds or removes op status for the given XUID and persists it.
func (s *OpSet) SetOp(xuid string, op bool) error {
	s.mu.Lock()
	if op {
		s.data[xuid] = true
	} else {
		delete(s.data, xuid)
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// ---------------------------------------------------------------------
// Bans
// ---------------------------------------------------------------------

// BanSet tracks banned XUIDs and their reasons, persisted to bans.json.
type BanSet struct {
	mu   sync.RWMutex
	path string
	data map[string]string // xuid -> reason
}

// LoadBans reads the ban set from path. If the file doesn't exist, an empty
// set is returned.
func LoadBans(path string) (*BanSet, error) {
	s := &BanSet{path: path, data: map[string]string{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

// Reason returns the ban reason for the given XUID and whether they're
// banned at all.
func (s *BanSet) Reason(xuid string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[xuid]
	return r, ok
}

// Ban bans the given XUID with the given reason and persists it.
func (s *BanSet) Ban(xuid, reason string) error {
	s.mu.Lock()
	s.data[xuid] = reason
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

