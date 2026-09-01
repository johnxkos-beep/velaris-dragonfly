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

// DefaultRankName is the rank a player has until an op assigns them one —
// matches the PocketMine-MP "Ranks" plugin, which auto-assigns brand new
// players "Elite" (see RankModule::onJoin).
const DefaultRankName = "Elite"

// RankDef is a single rank's display definition. TagColor, ChatColor, and
// MessageColor default to the same value for a given rank (matching the
// PMMP original, which only had one prefix/name color per rank plus a
// separately-configurable chat message color) but can be edited
// independently in-game via /rank → Rank Colors.
type RankDef struct {
	Name         string
	TagColor     string // §-code for the floating name tag above the player's head.
	ChatColor    string // §-code for the "[Rank] Name" portion of chat messages.
	MessageColor string // §-code for the message text itself (user-editable per rank in PMMP).
}

// NameTagPrefix returns the text prepended to a player's floating name
// tag. Matches the PMMP original's format exactly: gray brackets, a
// colored rank name, then the color carried into the player's own name —
// e.g. "§7[§4Owner§7] §4PlayerName".
func (r RankDef) NameTagPrefix() string {
	return fmt.Sprintf("§7[%s%s§7] %s", r.TagColor, r.Name, r.TagColor)
}

// FormatChat builds a full chat line for a message from a player holding
// this rank, matching the PMMP RankChatFormatter exactly:
// "§7[<ChatColor>Rank§7] <ChatColor>Name§7: <MessageColor>message".
func (r RankDef) FormatChat(playerName, message string) string {
	return fmt.Sprintf("§7[%s%s§7] %s%s§7: %s%s", r.ChatColor, r.Name, r.ChatColor, playerName, r.MessageColor, message)
}

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
		// Matches RankManager\RankModule::RANKS in the PMMP "Ranks" plugin
		// exactly (name + color): Owner=DARK_RED, Admin=GOLD,
		// YouTube=RED, Mod=YELLOW, Champion=WHITE, Elite=WHITE (the
		// default rank). TagColor/ChatColor/MessageColor all start equal
		// per rank, same as the PMMP original's colorsConfig seeding.
		defaults := []*RankDef{
			{Name: "Owner", TagColor: "§4", ChatColor: "§4", MessageColor: "§4"},
			{Name: "Admin", TagColor: "§6", ChatColor: "§6", MessageColor: "§6"},
			{Name: "YouTube", TagColor: "§c", ChatColor: "§c", MessageColor: "§c"},
			{Name: "Mod", TagColor: "§e", ChatColor: "§e", MessageColor: "§e"},
			{Name: "Champion", TagColor: "§f", ChatColor: "§f", MessageColor: "§f"},
			{Name: DefaultRankName, TagColor: "§f", ChatColor: "§f", MessageColor: "§f"},
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
	if s.migrateToRankManagerDefaults() {
		return s, s.save()
	}
	return s, nil
}

// migrateToRankManagerDefaults upgrades a rank_defs.json written before
// this file matched the PMMP "Ranks" plugin's rank list one-time, so
// existing servers don't need to delete rank_defs.json by hand to pick up
// the fix. It only adds ranks that are missing and renames the old
// "Default" rank to "Elite" (carrying its colors over) — it never
// overwrites colors on a rank that already exists under its PMMP name, so
// any in-game recoloring already done via /rank is preserved. Returns
// true if anything changed (caller should persist).
func (s *RankDefSet) migrateToRankManagerDefaults() bool {
	changed := false

	if old, ok := s.defs["Default"]; ok {
		if _, hasElite := s.defs["Elite"]; !hasElite {
			old.Name = DefaultRankName
			s.defs[DefaultRankName] = old
			for i, n := range s.order {
				if n == "Default" {
					s.order[i] = DefaultRankName
				}
			}
		}
		delete(s.defs, "Default")
		changed = true
	}

	pmDefaults := []*RankDef{
		{Name: "Owner", TagColor: "§4", ChatColor: "§4", MessageColor: "§4"},
		{Name: "Admin", TagColor: "§6", ChatColor: "§6", MessageColor: "§6"},
		{Name: "YouTube", TagColor: "§c", ChatColor: "§c", MessageColor: "§c"},
		{Name: "Mod", TagColor: "§e", ChatColor: "§e", MessageColor: "§e"},
		{Name: "Champion", TagColor: "§f", ChatColor: "§f", MessageColor: "§f"},
		{Name: DefaultRankName, TagColor: "§f", ChatColor: "§f", MessageColor: "§f"},
	}
	for _, d := range pmDefaults {
		if _, ok := s.defs[d.Name]; !ok {
			s.defs[d.Name] = d
			s.order = append(s.order, d.Name)
			changed = true
		}
	}

	// Backfill MessageColor on any rank saved before that field existed —
	// default it to that rank's existing ChatColor, same as the PMMP
	// original's colorsConfig seeding (message color starts equal to the
	// rank's tag color until an op changes it via /rank).
	for _, d := range s.defs {
		if d.MessageColor == "" {
			d.MessageColor = d.ChatColor
			changed = true
		}
	}

	return changed
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

// SetMessageColor changes a rank's chat MESSAGE (not prefix) color and
// persists the change — matches the "Change Chat Color" form in the PMMP
// "Ranks" plugin, which only ever recolored the message text, not the
// "[Rank] Name" prefix.
func (s *RankDefSet) SetMessageColor(name, code string) error {
	s.mu.Lock()
	d, ok := s.defs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown rank %q", name)
	}
	d.MessageColor = code
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
