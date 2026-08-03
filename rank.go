// Package rank implements a minimal permission-rank system, replacing the
// PermissionAttachment-based rank plugin from PMMP. Ranks are looked up by
// XUID and persisted to a JSON file so they survive restarts.
package rank

import (
	"encoding/json"
	"os"
	"sync"
)

// Rank represents a single permission tier.
type Rank struct {
	Name string
	Tag  string // Chat prefix shown before the player's name.
}

var (
	// Owner is the highest rank, full access to everything.
	Owner = Rank{Name: "Owner", Tag: "§4[Owner]§r"}
	// Admin has staff-level access.
	Admin = Rank{Name: "Admin", Tag: "§c[Admin]§r"}
	// YouTube is a cosmetic/creator rank.
	YouTube = Rank{Name: "YouTube", Tag: "§c[YT]§r"}
	// Default is applied to every player with no rank on file.
	Default = Rank{Name: "Default", Tag: "§7[Member]§r"}
)

// ChatTag returns the chat prefix for the rank.
func (r Rank) ChatTag() string {
	return r.Tag
}

// byName maps a rank's Name field back to the Rank value, used when loading
// from disk.
var byName = map[string]Rank{
	Owner.Name:   Owner,
	Admin.Name:   Admin,
	YouTube.Name: YouTube,
	Default.Name: Default,
}

// Set holds the XUID -> rank-name mapping for the whole server, and knows
// how to persist itself to disk.
type Set struct {
	mu   sync.RWMutex
	path string
	data map[string]string // xuid -> rank name
}

// Load reads the rank set from the JSON file at path. If the file does not
// exist, an empty (but valid) Set is returned instead of an error.
func Load(path string) (*Set, error) {
	s := &Set{path: path, data: map[string]string{}}

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

// Of returns the Rank for the given XUID, falling back to Default if the
// player has no rank on file.
func (s *Set) Of(xuid string) Rank {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.data[xuid]
	if !ok {
		return Default
	}
	if r, ok := byName[name]; ok {
		return r
	}
	return Default
}

// Set assigns a rank to the given XUID and persists the change to disk.
func (s *Set) Set(xuid string, r Rank) error {
	s.mu.Lock()
	s.data[xuid] = r.Name
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}
