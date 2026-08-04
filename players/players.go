package players

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/ranks"
	"velaris-dragonfly/state"
)

// PlayerHandler is attached to every player that joins the server. It
// embeds player.NopHandler so we only need to implement the methods we
// actually care about — every other event is a silent no-op until we add
// it here.
type PlayerHandler struct {
	player.NopHandler

	p        *player.Player
	ranks    *ranks.RankSet
	rankDefs *ranks.RankDefSet
	log      *slog.Logger
}

// NewPlayerHandler creates a PlayerHandler for the given player.
func NewPlayerHandler(p *player.Player, rks *ranks.RankSet, rankDefs *ranks.RankDefSet, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: rks, rankDefs: rankDefs, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist per-player state.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	state.TrackQuit(p)
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
}

// HandleChat tags every chat message with the player's rank prefix.
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	name := h.ranks.Of(h.p.XUID())
	def, ok := h.rankDefs.Get(name)
	if !ok {
		def, _ = h.rankDefs.Get(ranks.DefaultRankName)
	}
	*message = fmt.Sprintf("%s %s", def.ChatPrefix(), *message)
}

// HandleHurt is called every time the player takes damage, before it is
// applied. Example here: reduce fall damage by 50%.
func (h *PlayerHandler) HandleHurt(ctx *player.Context, damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	if _, ok := src.(entity.FallDamageSource); ok {
		*damage *= 0.5
	}
}

// HandleDeath is called when the player dies.
func (h *PlayerHandler) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	h.log.Info("player died", "name", p.Name())
}
