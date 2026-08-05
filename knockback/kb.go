package knockback

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// lastAttack tracks, per XUID, the last time that player landed a hit —
// used to enforce AttackCooldownTicks. Replaces the original plugin's
// EntityDamageEvent MODIFIER_PREVIOUS_DAMAGE_COOLDOWN trick, which doesn't
// exist in Dragonfly; this cancels repeat hits outright instead, which is
// the closest equivalent to "cooldown governs hit timing".
var (
	attackMu   sync.Mutex
	lastAttack = map[string]time.Time{}
)

// OnAttackEntity should be called from PlayerHandler.HandleAttackEntity.
// It applies the configured knockback force/height (capped by the height
// limit) and enforces the attack cooldown by cancelling the hit entirely
// if the attacker is still on cooldown.
//
// NOTE: HandleAttackEntity's exact signature is my best read of the
// current Dragonfly player.Handler interface, following the same style as
// the other Handle* methods in players/players.go. If it doesn't compile,
// paste the exact error and it's a quick fix.
func OnAttackEntity(ctx *player.Context, attacker *player.Player, force, height *float64, critical *bool) {
	if Cfg == nil {
		return
	}
	s := Cfg.Snapshot()

	if s.AttackCooldownTicks > 0 {
		cooldown := time.Duration(s.AttackCooldownTicks) * 50 * time.Millisecond
		id := attacker.XUID()
		now := time.Now()

		attackMu.Lock()
		last, onCooldown := lastAttack[id]
		if onCooldown && now.Sub(last) < cooldown {
			attackMu.Unlock()
			ctx.Cancel()
			return
		}
		lastAttack[id] = now
		attackMu.Unlock()
	}

	*force = s.Horizontal
	*height = s.Vertical
	if *height > s.HeightLimit {
		*height = s.HeightLimit
	}
}

// OnHurt should be called from PlayerHandler.HandleHurt. It plays the
// configurable "ding" sound to a player's shooter when their projectile
// lands a hit — port of the original plugin's Ding feature.
//
// NOTE: entity.ProjectileDamageSource and its Owner field are my best read
// of the current damage source types — if the field is actually named
// something else (e.g. Shooter), paste the compiler error and it's a
// one-line fix.
func OnHurt(src world.DamageSource) {
	if Cfg == nil {
		return
	}
	s := Cfg.Snapshot()
	if !s.DingEnabled {
		return
	}

	pds, ok := src.(entity.ProjectileDamageSource)
	if !ok {
		return
	}
	shooter, ok := pds.Owner.(*player.Player)
	if !ok {
		return
	}

	pitch := s.DingPitch
	if pitch < 0 {
		pitch = 0
	} else if pitch > 24 {
		pitch = 24
	}
	shooter.PlaySound(sound.Note{Instrument: sound.Bell(), Pitch: pitch})
}

// ClearPlayer removes any cooldown state tracked for the given XUID. Call
// this from PlayerHandler.HandleQuit (alongside state.TrackQuit) so this
// package doesn't slowly leak memory over a long-running server.
func ClearPlayer(xuid string) {
	attackMu.Lock()
	delete(lastAttack, xuid)
	attackMu.Unlock()

	projMu.Lock()
	delete(lastShot, xuid)
	projMu.Unlock()
}
