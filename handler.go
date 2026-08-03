// Package handler contains the central player.Handler implementation for
// the server. This is the Dragonfly equivalent of PMMP's event listeners:
// every hook you need (join, quit, chat, damage, death, block break, etc.)
// gets filled in here instead of being spread across separate listener
// classes.
package handler

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/internal/rank"
)

// PlayerHandler is attached to every player that joins the server (see
// main.go's srv.Accept() loop). It embeds player.NopHandler so we only need
// to implement the methods we actually care about — every other event is a
// silent no-op until we add it here.
type PlayerHandler struct {
	player.NopHandler

	p     *player.Player
	ranks *rank.Set
	log   *slog.Logger
}

// New creates a PlayerHandler for the given player.
func New(p *player.Player, ranks *rank.Set, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: ranks, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist player data, clean up trackers, etc.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
	// TODO: persist any per-player state (kills, stats, etc.) here.
}

// HandleChat is called whenever the player sends a chat message. ctx.Cancel()
// can be used to block the message entirely (e.g. a muted player).
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	r := h.ranks.Of(h.p.XUID())
	*message = fmt.Sprintf("%s %s", r.ChatTag(), *message)
}

// HandleHurt is called every time the player takes damage, before it is
// applied. This is the hook you'd use for things like fall damage reduction,
// PvP zone enforcement, or KOTH-specific damage rules.
func (h *PlayerHandler) HandleHurt(ctx *player.Context, damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	// Example: reduce fall damage by 50%. Replace with your real logic once
	// you port HopliteLegendary's fall damage system.
	if _, ok := src.(entity.FallDamageSource); ok {
		*damage *= 0.5
	}
}

// HandleDeath is called when the player dies. keepInv controls whether the
// player's inventory is kept on death instead of dropped.
func (h *PlayerHandler) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	h.log.Info("player died", "name", p.Name())
	// TODO: kill-attribution logic (e.g. Midas Sword fallback) goes here.
}
