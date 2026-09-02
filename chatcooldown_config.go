// Package chatcooldown is a port of the PocketMine-MP "ChatCooldown"
// plugin (sergittos\chatcooldown): makes players wait a configurable
// number of seconds between chat messages, editable in-game via
// /cooldown. Ops bypass the cooldown entirely — port of the original
// "chatcooldown.bypass" permission, which defaulted to op; "chatcooldown
// .admin" (command access) also defaulted to op, ported here as
// state.IsOpSource, same as every other admin-only command in this
// project.
//
// The original tracked per-player state as a Session object created on
// PlayerLoginEvent and destroyed on quit (session/Session.php +
// SessionFactory.php). That's collapsed here into a single map keyed by
// XUID (see chatcooldown.go), the same simplification knockback.go
// applies to its own per-player attack-cooldown tracking.
package chatcooldown

import (
	"encoding/json"
	"os"
	"sync"
)

// settings is the persisted configuration, ported field-for-field from
// the original plugin's resources/config.yml. JSON is used instead of
// YAML to match every other config file in this project (ranks.json,
// kb.json, ...).
type settings struct {
	Seconds int `json:"cooldown_seconds"`

	// Message may contain the literal substring "(time)", replaced with
	// the number of seconds still remaining — same placeholder the
	// original config.yml used. The original also supported {RED} /
	// {GREEN} / ... color placeholders via a custom ColorUtils
	// translator; that's dropped here in favor of raw "§" codes typed
	// directly into the message, matching how every other in-game
	// message in this project (including every other config-file
	// default, e.g. knockback's ProjectileCooldownMessage) is already
	// written.
	Message string `json:"cooldown_message"`
}

// defaultSettings mirrors the stock config.yml shipped with the original
// plugin (cooldown: 3, same message text minus the {RED} placeholder,
// replaced with the equivalent literal §c).
func defaultSettings() settings {
	return settings{
		Seconds: 3,
		Message: "§cYou must wait (time) seconds to chat again!",
	}
}

// Config is the active, hot-editable chat-cooldown configuration. Safe
// for concurrent use from the /cooldown form handler and from the chat
// handler on other goroutines.
type Config struct {
	mu   sync.RWMutex
	path string
	s    settings
}

// Cfg is the single active Config, set once in main() via
// chatcooldown.Load before the server starts accepting players — same
// pattern as knockback.Cfg / pvp.Cfg / restrict.Cfg. Every other file in
// this package reads through this.
var Cfg *Config

// Load reads the config from the JSON file at path, creating it with
// defaults if it doesn't exist yet. Call this once from main() before
// srv.Accept(), then assign the result to chatcooldown.Cfg, e.g.:
//
//	chatcooldown.Cfg, err = chatcooldown.Load("chatcooldown.json")
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

// Save replaces the current settings and persists them to disk. Called
// by the /cooldown form's Submit handler.
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
