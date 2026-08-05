// Package knockback is a port of the original PocketMine-MP "CustomKB"
// plugin (Phqzing\CustomKB): configurable melee knockback + attack
// cooldown + projectile-shoot cooldown + a "ding" sound on projectile
// hits, all editable in-game via /kb.
//
// PMMP required subclassing Player and hooking DataPacketSendEvent to
// pull this off; Dragonfly exposes cleaner per-attack hooks
// (HandleAttackEntity, HandleItemUse) so most of this is simpler here.
// The one PMMP feature that doesn't have a public Dragonfly equivalent —
// stripping the vanilla melee "hit" sound packet — is called out below
// where it's kept as an unenforced config field for continuity.
package knockback

import (
	"encoding/json"
	"os"
	"sync"
)

// settings is the persisted configuration, ported field-for-field from
// the original plugin's config.yml (see that file's comments for what
// each value does). JSON is used instead of YAML to match every other
// config file in this project (ranks.json, ops.json, bans.json, ...).
type settings struct {
	Horizontal                float64 `json:"kb_horizontal"`
	Vertical                  float64 `json:"kb_vertical"`
	HeightLimit               float64 `json:"kb_height_limit"`
	AttackCooldownTicks       int     `json:"attack_cooldown_ticks"`
	ProjectileCooldownEnabled bool    `json:"projectile_cooldown_enabled"`
	ProjectileCooldownSeconds float64 `json:"projectile_cooldown_seconds"`
	ProjectileCooldownMessage string  `json:"projectile_cooldown_message"`

	// RemoveHitSound is kept for config-file continuity with the original
	// plugin but is currently UNENFORCED: Dragonfly doesn't expose a
	// public hook to intercept the outgoing melee "hit" sound packet the
	// way PMMP's DataPacketSendEvent did. Left in (and still shown as a
	// toggle in the /kb form) in case a future Dragonfly version adds a
	// way to do this.
	RemoveHitSound bool `json:"remove_hit_sound"`

	DingEnabled bool `json:"ding_enabled"`
	// DingPitch replaces the original ding-xp-level (0-30, XP-orb-pickup
	// sound pitch). Dragonfly has no XP-orb-pickup sound with adjustable
	// pitch, so the ding is played as a note-block "bell" sound instead —
	// DingPitch is a note-block pitch, 0-24. See kb.go.
	DingPitch int `json:"ding_pitch"`
}

// defaultSettings mirrors the stock config.yml shipped with the original
// plugin.
func defaultSettings() settings {
	return settings{
		Horizontal:                0.4,
		Vertical:                  0.4,
		HeightLimit:               0.4,
		AttackCooldownTicks:       10,
		ProjectileCooldownEnabled: true,
		ProjectileCooldownSeconds: 2.5,
		ProjectileCooldownMessage: "§cWait before shooting again!",
		RemoveHitSound:            true,
		DingEnabled:               true,
		DingPitch:                 12,
	}
}

// Config is the active, hot-editable knockback/cooldown/sound
// configuration. Safe for concurrent use from the /kb form handler and
// from player event handlers on other goroutines.
type Config struct {
	mu   sync.RWMutex
	path string
	s    settings
}

// Cfg is the single active Config, set once in main() via knockback.Load
// before the server starts accepting players — same pattern as
// state.Ranks / state.Ops / state.Bans. Every other file in this package
// reads through this.
var Cfg *Config

// Load reads the config from the JSON file at path, creating it with
// defaults if it doesn't exist yet. Call this once from main() before
// srv.Accept(), then assign the result to knockback.Cfg, e.g.:
//
//	kbCfg, err := knockback.Load("kb.json")
//	...
//	knockback.Cfg = kbCfg
func Load(path string) (*Config, error) {
	c := &Config{path: path, s: defaultSettings()}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, c.save(c.s)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &c.s); err != nil {
		return nil, err
	}
	return c, nil
}

// Snapshot returns a copy of the current settings, safe to read without
// holding any lock.
func (c *Config) Snapshot() settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.s
}

// Save replaces the current settings and persists them to disk. Called by
// the /kb form's Submit handler.
func (c *Config) Save(s settings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s = s
	return c.save(s)
}

func (c *Config) save(s settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0644)
}
