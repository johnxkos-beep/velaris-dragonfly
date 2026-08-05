package knockback

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
)

// lastShot tracks, per XUID, the last time that player used a projectile
// item — used to enforce ProjectileCooldownSeconds.
var (
	projMu   sync.Mutex
	lastShot = map[string]time.Time{}
)

// isProjectileItem reports whether it is one of the throwable/shootable
// items the original plugin's blanket ProjectileLaunchEvent hook covered.
// Dragonfly has no single "about to launch a projectile" event, so the
// cooldown is enforced earlier instead, on HandleItemUse, keyed off the
// held item's type.
//
// NOTE: these type names are my best read of the current item package —
// if any of them don't exist under this exact name in your Dragonfly
// version, paste the compiler error and it's a quick fix (or just delete
// the offending case, the rest still works).
func isProjectileItem(it item.Item) bool {
	switch it.(type) {
	case item.Snowball, item.Egg, item.EnderPearl, item.SplashPotion, item.LingeringPotion,
		item.ExperienceBottle, item.Bow, item.Crossbow, item.Trident:
		return true
	}
	return false
}

// OnItemUse should be called from PlayerHandler.HandleItemUse. It enforces
// the configured cooldown between shooting/throwing projectiles.
func OnItemUse(ctx *player.Context, p *player.Player) {
	if Cfg == nil {
		return
	}
	s := Cfg.Snapshot()
	if !s.ProjectileCooldownEnabled {
		return
	}

	held, _ := p.HeldItems()
	if !isProjectileItem(held.Item()) {
		return
	}

	cooldown := time.Duration(s.ProjectileCooldownSeconds * float64(time.Second))
	if cooldown <= 0 {
		return
	}

	id := p.XUID()
	now := time.Now()

	projMu.Lock()
	last, onCooldown := lastShot[id]
	if onCooldown && now.Sub(last) < cooldown {
		projMu.Unlock()
		ctx.Cancel()
		if s.ProjectileCooldownMessage != "" {
			p.Message(s.ProjectileCooldownMessage)
		}
		return
	}
	lastShot[id] = now
	projMu.Unlock()
}
