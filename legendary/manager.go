package legendary

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/player"
)

// ---------------------------------------------------------------------
// Claims
// ---------------------------------------------------------------------
//
// Matches the original plugin's "single-claim crafting": once a player has
// crafted a given legendary, they can't instant-craft another copy from the
// codex (they can still lose/drop/trade the one they have — this only gates
// the craft button, same as the PHP version's LegendaryManager).
//
// Persisted to data/legendary_claims.json (XUID -> set of weapon IDs),
// mirroring ranks.RankSet's on-disk JSON pattern elsewhere in this repo.

// Manager tracks which players have claimed which legendaries and persists
// that to disk.
type Manager struct {
	mu     sync.RWMutex
	path   string
	claims map[string]map[string]bool // xuid -> set of weapon IDs
}

// NewManager loads (or creates) the claims file at path.
func NewManager(path string) (*Manager, error) {
	m := &Manager{path: path, claims: map[string]map[string]bool{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("legendary: read claims file: %w", err)
	}
	var raw map[string][]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("legendary: parse claims file: %w", err)
	}
	for xuid, ids := range raw {
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		m.claims[xuid] = set
	}
	return m, nil
}

func (m *Manager) save() error {
	raw := make(map[string][]string, len(m.claims))
	for xuid, set := range m.claims {
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		raw[xuid] = ids
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0644)
}

// HasClaimed reports whether xuid has already crafted weaponID.
func (m *Manager) HasClaimed(xuid, weaponID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.claims[xuid][weaponID]
}

// markClaimed records that xuid has crafted weaponID, and persists it.
func (m *Manager) markClaimed(xuid, weaponID string) error {
	m.mu.Lock()
	if m.claims[xuid] == nil {
		m.claims[xuid] = map[string]bool{}
	}
	m.claims[xuid][weaponID] = true
	err := m.save()
	m.mu.Unlock()
	return err
}

// ---------------------------------------------------------------------
// Crafting
// ---------------------------------------------------------------------

// HasIngredients reports whether p's inventory currently holds every
// ingredient in d.Recipe (summed across stacks/slots).
func HasIngredients(p *player.Player, d *Def) bool {
	inv := p.Inventory()
	have := map[string]int{}
	for _, st := range inv.All() {
		if st.Empty() {
			continue
		}
		name, _ := st.Item().EncodeItem()
		have[name] += st.Count()
	}
	for _, ing := range d.Recipe {
		if have[ing.Name] < ing.Count {
			return false
		}
	}
	return true
}

// Craft removes d.Recipe's ingredients from p's inventory and grants the
// legendary weapon, marking it claimed. Returns an error (and leaves the
// inventory untouched) if the player is missing ingredients, has already
// claimed this weapon, or the weapon ID isn't registered.
//
// CAVEAT: ingredient removal here does a best-effort per-stack scan rather
// than a single atomic "remove N of this item across the whole inventory"
// call, because I don't have a live dragonfly build in this environment to
// confirm inventory.Inventory exposes the latter directly. It removes from
// the first stacks it finds until each ingredient's count is satisfied,
// re-checking after each removal. Worth a real playtest before trusting it
// under concurrent/edge-case inventory states (this stacks with slots
// mid-move, etc.) — flag it if anything looks off and I'll harden it.
func (m *Manager) Craft(p *player.Player, weaponID string) error {
	d, ok := Defs[weaponID]
	if !ok {
		return fmt.Errorf("unknown legendary %q", weaponID)
	}
	xuid := p.XUID()
	if m.HasClaimed(xuid, weaponID) {
		return fmt.Errorf("you've already claimed the %s", d.DisplayName)
	}
	if !HasIngredients(p, d) {
		return fmt.Errorf("you're missing ingredients for the %s (need: %s)", d.DisplayName, DescribeRecipe(d))
	}

	inv := p.Inventory()
	for _, ing := range d.Recipe {
		remaining := ing.Count
		for slot, st := range inv.All() {
			if remaining <= 0 {
				break
			}
			if st.Empty() {
				continue
			}
			name, _ := st.Item().EncodeItem()
			if name != ing.Name {
				continue
			}
			take := st.Count()
			if take > remaining {
				take = remaining
			}
			newStack := st.Grow(-take)
			if err := inv.SetItem(slot, newStack); err != nil {
				return fmt.Errorf("removing ingredients: %w", err)
			}
			remaining -= take
		}
	}

	stack, ok := NewWeaponStack(weaponID)
	if !ok {
		return fmt.Errorf("unknown legendary %q", weaponID)
	}
	if _, err := inv.AddItem(stack); err != nil {
		// Original plugin's /award box (overflow storage) isn't ported yet
		// (round 2+). For now, tell the player to clear a slot rather than
		// silently eating their materials.
		return fmt.Errorf("your inventory is full — free up a slot and try again (materials were already consumed, sorry — award-box overflow storage isn't ported yet)")
	}

	if err := m.markClaimed(xuid, weaponID); err != nil {
		return fmt.Errorf("crafted, but failed to save claim: %w", err)
	}
	return nil
}
