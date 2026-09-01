package ranks

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/player"
)

// ---------------------------------------------------------------------
// Ranks
// ---------------------------------------------------------------------
//
// Two separate concerns live here:
//   - RankDefSet: the ranks that exist and how they're displayed — a name
//     plus two colors. TagColor colors the floating name tag shown above a
//     player in the world; ChatColor colors the prefix shown in the chat
//     window when they talk. Both are editable in-game via /rank (see the
//     "Rank Colors" section further down) and persisted to disk.
//   - RankSet: which rank each player (by XUID) currently has. Also
//     editable via /rank ("Set Rank" / "Remove Rank") and persisted.
//
// DefaultRankName is what a player has until an op assigns them one.

const DefaultRankName = "Default"

// RankDef is a single rank's display definition.
type RankDef struct {
	Name      string
	TagColor  string // §-code applied to the name tag shown above the player.
	ChatColor string // §-code applied to the prefix shown in chat.
}

// NameTagPrefix returns the text prepended to a player's floating name tag.
func (r RankDef) NameTagPrefix() string { return fmt.Sprintf("%s[%s]§r ", r.TagColor, r.Name) }

// ChatPrefix returns the text prepended to a player's chat messages.
func (r RankDef) ChatPrefix() string { return fmt.Sprintf("%s[%s]§r", r.ChatColor, r.Name) }

// RankDefSet holds every rank's definition and persists it to disk. Player
// -> rank assignment lives separately in RankSet, below.
type RankDefSet struct {
	mu    sync.RWMutex
	path  string
	defs  map[string]*RankDef
	order []string // display order for menus; also acts as the set of valid rank names
}

// LoadRankDefs reads rank definitions from the JSON file at path. If the
// file does not exist, four sensible defaults are seeded and saved.
func LoadRankDefs(path string) (*RankDefSet, error) {
	s := &RankDefSet{path: path, defs: map[string]*RankDef{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		defaults := []*RankDef{
			{Name: "Owner", TagColor: "§4", ChatColor: "§4"},
			{Name: "Admin", TagColor: "§c", ChatColor: "§c"},
			{Name: "YouTube", TagColor: "§c", ChatColor: "§c"},
			{Name: DefaultRankName, TagColor: "§7", ChatColor: "§7"},
		}
		for _, d := range defaults {
			s.defs[d.Name] = d
			s.order = append(s.order, d.Name)
		}
		return s, s.save()
	} else if err != nil {
		return nil, err
	}

	var raw struct {
		Order []string
		Defs  map[string]*RankDef
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	s.order, s.defs = raw.Order, raw.Defs
	return s, nil
}

func (s *RankDefSet) save() error {
	raw := struct {
		Order []string
		Defs  map[string]*RankDef
	}{Order: s.order, Defs: s.defs}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// Names returns every rank name, in display order.
func (s *RankDefSet) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Get returns the definition for the named rank, if it exists.
func (s *RankDefSet) Get(name string) (RankDef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[name]
	if !ok {
		return RankDef{}, false
	}
	return *d, true
}

// SetTagColor changes a rank's name-tag color and persists the change.
func (s *RankDefSet) SetTagColor(name, code string) error {
	s.mu.Lock()
	d, ok := s.defs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown rank %q", name)
	}
	d.TagColor = code
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetChatColor changes a rank's chat-prefix color and persists the change.
func (s *RankDefSet) SetChatColor(name, code string) error {
	s.mu.Lock()
	d, ok := s.defs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown rank %q", name)
	}
	d.ChatColor = code
	err := s.save()
	s.mu.Unlock()
	return err
}

// RankSet holds the XUID -> rank-name mapping for the whole server, and
// knows how to persist itself to disk.
type RankSet struct {
	mu   sync.RWMutex
	path string
	data map[string]string // xuid -> rank name
}

// LoadRanks reads the rank set from the JSON file at path. If the file does
// not exist, an empty (but valid) RankSet is returned instead of an error.
func LoadRanks(path string) (*RankSet, error) {
	s := &RankSet{path: path, data: map[string]string{}}

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

// Of returns the rank name for the given XUID, falling back to
// DefaultRankName if the player has no rank on file.
func (s *RankSet) Of(xuid string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.data[xuid]
	if !ok {
		return DefaultRankName
	}
	return name
}

// SetRank assigns a rank (by name) to the given XUID and persists the
// change to disk.
func (s *RankSet) SetRank(xuid, rankName string) error {
	s.mu.Lock()
	s.data[xuid] = rankName
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// ApplyNameTag recomputes and sets a player's floating name tag from their
// current rank. Call this on join and any time a player's rank or a rank's
// TagColor changes.
func ApplyNameTag(p *player.Player, ranks *RankSet, defs *RankDefSet) {
	name := ranks.Of(p.XUID())
	def, ok := defs.Get(name)
	if !ok {
		def, _ = defs.Get(DefaultRankName)
	}
	p.SetNameTag(def.NameTagPrefix() + p.Name())
}
