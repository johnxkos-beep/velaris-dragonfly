package dragonfly

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	pmmpcompat "velaris-dragonfly/pmmpclient"
)

// HandleBlockPlace forwards a block placement to PHP (e.g. HopliteLegendary's
// ZoneListener/BuilderListener checking for the orange/black/protected-zone
// concrete) and cancels the placement in Dragonfly if PHP says to.
func (h *Handler) HandleBlockPlace(ctx *player.Context, pos cube.Pos, b world.Block) {
	callCtx, cancel := h.rt.context()
	defer cancel()

	name, _ := b.EncodeBlock()
	position := pmmpcompat.Position{X: float64(pos.X()), Y: float64(pos.Y()), Z: float64(pos.Z())}

	result, actions, err := h.rt.client.BlockPlace(callCtx, h.p.UUID().String(), h.p.Name(), position, &pmmpcompat.Block{TypeID: name}, nil)
	if err != nil {
		h.rt.report(err)
		return
	}
	if result.Cancelled {
		ctx.Cancel()
	}
	h.rt.applyActions(callCtx, actions)
}

// HandleBlockBreak forwards a block break to PHP (e.g. checking whether the
// block is protected by BuilderManager, or removing a pvp/restrict zone if
// it was the marker block) and cancels the break in Dragonfly if PHP says
// to.
func (h *Handler) HandleBlockBreak(ctx *player.Context, pos cube.Pos, drops *[]item.Stack, xp *int) {
	callCtx, cancel := h.rt.context()
	defer cancel()

	// NOTE: do not call h.p.Tx().Block(pos) here — by the time this
	// handler runs, Dragonfly's break transaction has already finished,
	// and touching Tx() panics ("use of transaction after transaction
	// finishes is not permitted"), crashing the whole server. See the
	// identical note in players/autosmelt.go. Dragonfly already
	// computed the drops before calling this handler, so use the first
	// drop's item id as a stand-in for the block that broke — for
	// blocks like concrete that drop themselves, this is exactly the
	// same identifier the block itself would've encoded to.
	name := ""
	if drops != nil && len(*drops) > 0 {
		name, _ = (*drops)[0].Item().EncodeItem()
	}
	position := pmmpcompat.Position{X: float64(pos.X()), Y: float64(pos.Y()), Z: float64(pos.Z())}

	result, actions, err := h.rt.client.BlockBreak(callCtx, h.p.UUID().String(), h.p.Name(), position, &pmmpcompat.Block{TypeID: name}, nil)
	if err != nil {
		h.rt.report(err)
		return
	}
	if result.Cancelled {
		ctx.Cancel()
	}
	h.rt.applyActions(callCtx, actions)
}
