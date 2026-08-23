package legendary

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/player"
)

// ---------------------------------------------------------------------
// Claims
// ---------------------------------------------------------------------
//
// FIXED (was wrong in round 1): the original plugin's "single-claim
// crafting" is a GLOBAL, server-wide lock per weapon — LegendaryManager.php
// keys $claimed by weapon id => the one player's name who crafted it, and
// craft() bails out if isClaimed($id) is already true for ANYONE, full
// stop. It is not a per-player cooldown. Round 1 of this port tracked
// claims per-xuid (map[string]map[string]bool), which meant every player
// could craft their own personal copy of every legendary — the opposite of
// "only one Excalibur will ever exist on the server". That's the bug this
// pass fixes: claims are now weaponID -> claimant, checked and written
// without regard to who's asking.
//
// Persisted to data/legendary_claims.json (weapon ID -> claimant name),
// mirroring ranks.RankSet's on-disk JSON pattern elsewhere in this repo.

// Manager tracks which legendaries have been claimed (server-wide, one
// craft ever per weapon) and persists that to disk.
type Manager struct {
	mu     sync.RWMutex
	path   string
	claims map[string]string // weapon ID -> claimant player name
}

// NewManager loads (or creates) the claims file at path.
func NewManager(path string) (*Manager, error) {
	m := &Manager{path: path, claims: map[string]string{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("legendary: read claims file: %w", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("legendary: parse claims file: %w", err)
	}
	m.claims = raw
	return m, nil
}

func (m *Manager) save() error {
	b, err := json.MarshalIndent(m.claims, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0644)
}

// HasClaimed reports whether weaponID has already been crafted by anyone,
// server-wide.
func (m *Manager) HasClaimed(weaponID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.claims[weaponID]
	return ok
}

// ClaimedBy returns the name of the player who crafted weaponID, or ""
// if nobody has yet.
func (m *Manager) ClaimedBy(weaponID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.claims[weaponID]
}

// markClaimed records that playerName has crafted weaponID, and persists
// it. Fails (without writing) if weaponID is already claimed, to close a
// race between two players submitting Craft for the same weapon at once.
func (m *Manager) markClaimed(playerName, weaponID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, already := m.claims[weaponID]; already {
		return fmt.Errorf("already claimed")
	}
	m.claims[weaponID] = playerName
	return m.save()
}

// ---------------------------------------------------------------------
// Crafting
// ---------------------------------------------------------------------

// HasIngredients reports whether p's inventory currently holds every
// ingredient in d.Recipe (summed across stacks/slots).
func HasIngredients(p *player.Player, d *Def) bool {
	inv := p.Inventory()
	have := map[string]int{}
	for slot := 0; slot < inv.Size(); slot++ {
		st, err := inv.Item(slot)
		if err != nil || st.Empty() {
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
// legendary weapon, marking it claimed server-wide (nobody else — including
// p themselves again — can ever craft this weapon ID after this succeeds;
// matches the original PHP plugin's one-copy-per-server design). Returns an
// error (and leaves the inventory untouched) if the weapon is already
// claimed by anyone, the player is missing ingredients, or the weapon ID
// isn't registered.
func (m *Manager) Craft(p *player.Player, weaponID string) error {
	d, ok := Defs[weaponID]
	if !ok {
		return fmt.Errorf("unknown legendary %q", weaponID)
	}
	if m.HasClaimed(weaponID) {
		return fmt.Errorf("the %s has already been claimed by %s", d.DisplayName, m.ClaimedBy(weaponID))
	}
	if !HasIngredients(p, d) {
		return fmt.Errorf("you're missing ingredients for the %s (need: %s)", d.DisplayName, DescribeRecipe(d))
	}

	inv := p.Inventory()
	for _, ing := range d.Recipe {
		remaining := ing.Count
		for slot := 0; slot < inv.Size() && remaining > 0; slot++ {
			st, err := inv.Item(slot)
			if err != nil || st.Empty() {
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
	log.Printf("[legendary] crafting %s for %s: stack built with count=%d empty=%v", weaponID, p.Name(), stack.Count(), stack.Empty())
	n, err := inv.AddItem(stack)
	log.Printf("[legendary] AddItem(%s) for %s returned n=%d err=%v", weaponID, p.Name(), n, err)
	if err != nil {
		// Original plugin's /award box (overflow storage) isn't ported yet
		// (round 2+). For now, tell the player to clear a slot rather than
		// silently eating their materials.
		return fmt.Errorf("your inventory is full — free up a slot and try again (materials were already consumed, sorry — award-box overflow storage isn't ported yet)")
	}
	if n < 1 {
		// AddItem reported no error but also added nothing — treat that as
		// a failure too rather than letting Craft report success for
		// nothing. If you see this in the log, the stack being built by
		// NewWeaponStack is empty going in; paste the log line above back
		// to me.
		return fmt.Errorf("something went wrong granting the %s — nothing was added to your inventory even though no error was reported. Check the server console for a [legendary] log line and send it to me", d.DisplayName)
	}

	if err := m.markClaimed(p.Name(), weaponID); err != nil {
		// Someone else's Craft won the race between our HasClaimed check
		// above and now. Materials are already gone and the item is already
		// in the player's inventory — rather than silently taking it back
		// (which could desync a full inventory or a stack that merged into
		// an existing slot), leave it with them but don't record a second
		// claimant, and tell the player plainly what happened.
		log.Printf("[legendary] race: %s finished crafting %s but it was already claimed by %s", p.Name(), weaponID, m.ClaimedBy(weaponID))
		return fmt.Errorf("someone else claimed the %s a moment before you — sorry, you keep the one you just crafted, but the server now only recognizes %s's claim", d.DisplayName, m.ClaimedBy(weaponID))
	}
	return nil
}
