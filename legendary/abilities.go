// Weapon abilities, ported from AbilityListener.php (the source plugin's
// own reverse-engineered rebuild of the Hoplite Weapons add-on's
// scripts/main.js) plus, for Dragon Katana and Eagle Eye Bow (added in
// this pass, replacing Golem Hammer and Emerald Sword), read directly out
// of the add-on's own scripts/main.js this time. See the doc comments on
// each function below for the exact numbers/behavior each one is matched
// against.
//
// WHAT DIDN'T COME ACROSS 1:1, AND WHY:
//
//   - Mjolnir's/Poseidon Trident's "throw" fires a real flying projectile
//     entity (see projectile.go) using the add-on's own real physics
//     tuning — not an instant hitscan.
//     Eagle Eye Bow fires a REAL vanilla-style arrow entity via
//     Dragonfly's own built-in arrow constructor — real flight, real
//     collision, real ammo consumption from your inventory — but as an
//     INSTANT shot on click (see shootEagleEyeBow), not a draw-and-release
//     charge. A real hold-to-draw/release-to-fire mechanic (with power
//     scaling by hold time and a mid-air freeze effect while drawing,
//     matching the add-on's own script) was built and even correctly
//     wired to Dragonfly's real item.Releasable interface, but reverted
//     per explicit request after it still wasn't working as expected in
//     testing — simpler and confirmed-working won out over matching the
//     add-on's exact feel here.
//     HOW all of these are triggered changed too: they no longer go
//     through PlayerHandler.HandleItemUse at all (that handler doesn't
//     receive a *world.Tx in this Dragonfly version — confirmed by a real
//     crash log showing it panic on every single use, not just at
//     disconnect). Instead, Weapon in items.go implements item.Usable
//     directly (Use(tx, u, ctx) — the same interface
//     bosses/demonking/spawnegg.go's UsableOnBlock already proves works in
//     this exact codebase for UseOnBlock), which Dragonfly hands a
//     legitimate Tx to as a real parameter. See OnUse below for the 3
//     weapons that need it; everything self-only (Shadow Blade, Excalibur,
//     Dragon Katana, Trident's sneak-riptide) still goes through
//     OnItemUse/HandleItemUse exactly
//     like before, since those never needed Tx to begin with.
//   - A real flying-then-returning projectile entity (visually leaving
//     your hand and traveling to the target over time, the way the add-on's
//     own script does it) is still a bigger, separate build — same scale
//     as the DemonKing boss (its own Tick(tx) gets a legitimate Tx from
//     Dragonfly's entity scheduler, proven safe in this repo, but it's a
//     few hundred lines of movement/collision code). Worth doing later if
//     you want the weapon to visibly leave your hand instead of just
//     landing instantly.
//   - Dragon Katana's dash is a plain cooldown-gated ability (no charge
//     requirement) — see dragonKatanaDash's own doc comment for why that's
//     simpler than the add-on's original charge-building system.
//   - Midas Sword's growing power is tracked as an internal per-player
//     "power level" (0-6) rather than by actually writing real vanilla
//     Sharpness/Looting/Fire Aspect/Mending enchantments onto the item
//     NBT — Dragonfly's public enchantment-writing API on an item.Stack
//     wasn't confirmed against this version from the environment this was
//     written in (no network access to check). The bonus damage is the
//     same numbers Sharpness would have given; what's missing is the
//     enchant glint and the tooltip literally reading "Sharpness VI" in
//     your inventory.
//   - Anything the original did with raw Bedrock protocol packets
//     (spoofing your held item/armor to other viewers during Shadow
//     Blade's cloak) is dropped entirely — Dragonfly's real Invisibility
//     effect (see HandleHurt below) already hides the whole player model
//     properly client-side.
//
// NOT COVERED: mob kills toward Midas Sword's power (its point-per-kill
// mechanic is player-kills-only right now). This repo currently only has a
// death hook for PLAYER deaths (PlayerHandler.HandleDeath in
// players/players.go) — there's no mob-death hook wired up anywhere yet.
// Tell me if you want that added. (Dragon Katana no longer has a
// charge/kill mechanic at all — see dragonKatanaDash.)
//
// UNVERIFIED AGAINST A REAL BUILD: everything in this file compiles
// against my best read of Dragonfly v0.11.1's player/effect/entity APIs,
// following the same patterns already used elsewhere in this repo
// (knockback/kb.go, players/players.go, bosses/demonking/entity.go) — but
// I have no network access in this environment to `go build` it myself.
// If something doesn't compile, paste the exact error back and it's
// almost always a one-line fix (wrong field/method name).
package legendary

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// AttackPoints returns the base damage each legendary weapon deals.
// Shadow Blade/Crimson Chain Sword/Excalibur=Diamond Sword(7). Dragon
// Katana/Poseidon Trident=Netherite Sword-equivalent(8) — Poseidon
// Trident matches a real vanilla Trident's damage, which hits as hard as
// Netherite; Dragon Katana matches that same 8. Midas Sword is ALSO set
// to 8 (Netherite-equivalent) per explicit request — the original plugin
// source actually has Midas at 7 (Diamond Sword), so this is a
// deliberate deviation from the source, not a correction. Eagle Eye
// Bow=Bow(3). Mjolnir=Diamond Axe(9).
func AttackPoints(weaponID string) float64 {
	switch weaponID {
	case "bey:dragon_katana", "bey:poseidon_trident", "bey:midas_sword":
		return 8
	case "bey:mjolnir":
		return 9
	case "bey:eagle_eye_bow":
		return 3
	default:
		return 7
	}
}

// cooldownTicks matches each weapon's real cooldown (20 ticks/sec). Dragon
// Katana's dash cooldown is 200 ticks (10s) in the add-on's non-"chaos
// mode" path — the add-on also has a 60-tick chaos-mode variant this
// server doesn't have a concept of, so the normal 200 is used always.
// Eagle Eye Bow has no explicit weapon cooldown in the add-on itself
// (its own draw-time IS the pacing) — a modest 40-tick (2s) cooldown is
// added here since our version is an instant hitscan, not a real draw, to
// avoid it being spammable.
func cooldownTicks(weaponID string) int {
	switch weaponID {
	case "bey:mjolnir":
		return 900 // 45s
	case "bey:poseidon_trident":
		return 400 // 20s, shared by throw and Riptide-leap
	case "bey:shadow_blade", "bey:excalibur":
		return 1200 // 60s
	case "bey:dragon_katana":
		return 200 // 10s dash cooldown, no charge requirement
	case "bey:eagle_eye_bow":
		return 40 // 2s, see doc comment above
	default:
		return 0 // Midas Sword/Crimson Chain Sword: passive only, no interact ability
	}
}

// ---------------------------------------------------------------------
// Per-player ability state
// ---------------------------------------------------------------------

type playerAbilityState struct {
	mu sync.Mutex

	cooldownReady map[string]time.Time // weaponID -> when it's off cooldown

	shadowActive bool

	excaliburCharges int
	excaliburUntil   time.Time

	chainSwordWitherReady time.Time // Crimson Chain Sword: next tick a Wither proc is allowed

	midasPower int // 0-6, +1 per 50%-chance player kill, drives bonus damage
}

var (
	statesMu sync.Mutex
	states   = map[string]*playerAbilityState{} // xuid -> state
)

func stateFor(xuid string) *playerAbilityState {
	statesMu.Lock()
	defer statesMu.Unlock()
	s, ok := states[xuid]
	if !ok {
		s = &playerAbilityState{cooldownReady: map[string]time.Time{}}
		states[xuid] = s
	}
	return s
}

// ClearPlayer drops all tracked ability state for xuid. Call this from
// PlayerHandler.HandleQuit (same pattern as knockback.ClearPlayer) so this
// package doesn't leak memory over a long-running server.
func ClearPlayer(xuid string) {
	statesMu.Lock()
	delete(states, xuid)
	statesMu.Unlock()
}

func (s *playerAbilityState) onCooldown(weaponID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ready, ok := s.cooldownReady[weaponID]
	return ok && time.Now().Before(ready)
}

func (s *playerAbilityState) startCooldown(tx *world.Tx, p *player.Player, w legendaryItem, weaponID string, ticks int) {
	s.mu.Lock()
	s.cooldownReady[weaponID] = time.Now().Add(time.Duration(ticks) * 50 * time.Millisecond)
	s.mu.Unlock()
	applyCooldownUI(tx, p, w, ticks)
}

// applyCooldownUI puts w's icon under Dragonfly's real (gray) "on
// cooldown" hotbar overlay AND spawns the custom red bar HUD that matches
// what the add-on actually shows (see hud.go for the full story — the red
// bar is not a native Bedrock feature, it's a resource-pack trick). Both
// are purely cosmetic; the actual cooldown enforcement is
// startCooldown's cooldownReady map above, unaffected by either of these.
// ability actually works (that's startCooldown's cooldownReady map above,
// which was already correct and enforced before this existed).
//
// CONFIRMED against real Dragonfly docs this time (not a guess) — a
// previous pass guessed "ctx.SetCooldown" on *item.UseContext, which
// doesn't exist there (real build error). The actual method is on
// *player.Player: "func (p *Player) SetCooldown(item world.Item, cooldown
// time.Duration)", per
// https://pkg.go.dev/github.com/df-mc/dragonfly/server/player.
func applyCooldownUI(tx *world.Tx, p *player.Player, w legendaryItem, ticks int) {
	dur := time.Duration(ticks) * 50 * time.Millisecond
	p.SetCooldown(w, dur)
	StartCooldownBar(tx, p, w.WeaponDef().ID, dur)
}

// ---------------------------------------------------------------------
// Right-click / interact abilities — call from PlayerHandler.HandleItemUse
// ---------------------------------------------------------------------

// direction returns p's current look direction as a unit vector.
// UNVERIFIED: assumes *player.Player has a Rotation() cube.Rotation method
// (confirmed elsewhere in this repo, e.g. bosses/demonking/entity.go) and
// that cube.Rotation has a Vec3() converting yaw/pitch to a direction
// vector — the latter wasn't directly confirmed from this environment.
func direction(p *player.Player) mgl64.Vec3 {
	return p.Rotation().Vec3()
}

// OnUse handles every legendary weapon's active ability, including the
// "self-only" ones (Shadow Blade, Excalibur, Dragon Katana, Trident's
// sneak-riptide). Those used to run from PlayerHandler.HandleItemUse
// instead, on the theory that since they never fetch a *world.Tx
// explicitly, they didn't need one. They did — a crash log showed the
// exact same "world.Tx: use of transaction after transaction finishes"
// panic even for Shadow Blade, which only calls p.AddEffect(). So it's not
// just an explicit Tx() fetch that's unsafe there — any world-mutating
// Player method (AddEffect, SetVelocity, Teleport, at least) apparently
// needs a real Tx under the hood, and HandleItemUse doesn't have one to
// give it in this Dragonfly version. Every ability now goes through the
// one path proven to work instead: item.Usable's Use(tx, ...), same as
// bosses/demonking/spawnegg.go. PlayerHandler.HandleItemUse no longer
// calls into this package at all — see players/players.go. Called from
// Weapon.Use in items.go (item.Usable), which Dragonfly hands a real,
// working *world.Tx as a parameter — unlike PlayerHandler.HandleItemUse,
// confirmed broken for this by two separate real crash logs. Returns true
// if an ability actually fired.
func OnUse(tx *world.Tx, p *player.Player) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in OnUse for %s: %v", p.Name(), r)
		}
	}()

	held, _ := p.HeldItems()
	w, ok := held.Item().(legendaryItem)
	if !ok {
		return false
	}
	id := w.WeaponDef().ID
	ticks := cooldownTicks(id)
	if ticks == 0 {
		return false // Midas Sword/Crimson Chain Sword: no interact ability
	}

	s := stateFor(p.XUID())
	if s.onCooldown(id) {
		if id == "bey:poseidon_trident" {
			p.Message("§c§l[!] §r§cWeapon On Cooldown!")
		}
		return false
	}

	switch id {
	case "bey:mjolnir":
		SpawnProjectile(tx, p, "bey:mjolnir", 1.0)
		s.startCooldown(tx, p, w, "bey:mjolnir", 900)
	case "bey:poseidon_trident":
		if p.Sneaking() {
			dir := direction(p)
			p.SetVelocity(mgl64.Vec3{dir.X() * 8, 1.2, dir.Z() * 8})
			s.startCooldown(tx, p, w, "bey:poseidon_trident", 400)
			break
		}
		SpawnProjectile(tx, p, "bey:poseidon_trident", 0.35)
		s.startCooldown(tx, p, w, "bey:poseidon_trident", 400)
	case "bey:eagle_eye_bow":
		shootEagleEyeBow(tx, p, w, s)
	case "bey:shadow_blade":
		shadowBladeCloak(tx, p, w, s)
	case "bey:excalibur":
		excaliburShield(tx, p, w, s)
	case "bey:dragon_katana":
		dragonKatanaDash(tx, p, w, s)
	}
	return true
}

const rangedAbilityRange = 30.0

// findLookTarget scans nearby entities for the closest Player within a
// ~25-degree cone of where p is looking, within range blocks. Returns the
// player found (or nil) and the point to strike at (either the target's
// position or the max range point along the look direction).
//
// NOTE: only *player.Player targets are detected — mobs are never hit by
// these abilities. If you're testing solo with nobody else online, these
// 3 weapons will run (cooldown starts, a message prints either way now)
// but nothing will visibly happen, since there's nobody to strike. See
// strikeRanged below for the "why did nothing happen" message this now
// always sends regardless of whether anyone was actually hit.
func findLookTarget(p *player.Player, tx *world.Tx, rng float64) (*player.Player, mgl64.Vec3) {
	eye := p.Position().Add(mgl64.Vec3{0, 1.62, 0})
	dir := direction(p)
	fallback := eye.Add(dir.Mul(rng))

	var best *player.Player
	bestDist := rng
	for e := range tx.Entities() {
		other, ok := e.(*player.Player)
		if !ok || other == p {
			continue
		}
		toEntity := other.Position().Add(mgl64.Vec3{0, 1.62, 0}).Sub(eye)
		dist := toEntity.Len()
		if dist > rng || dist < 0.01 {
			continue
		}
		angle := toEntity.Normalize().Dot(dir)
		if angle > 0.9 && dist < bestDist {
			best = other
			bestDist = dist
		}
	}
	if best != nil {
		return best, best.Position()
	}
	return nil, fallback
}

// strikeRanged hits target (if found) for the weapon's base damage, and
// rolls lightningChance to also strike lightning (a particle+sound flash
// stand-in — see the file doc comment on lightning) at pos. Always
// messages the user either way, so "nothing visibly happened" (e.g.
// testing solo with no other player in range/cone) doesn't look
// indistinguishable from the ability silently failing.
//
// NOTE: no longer used by Eagle Eye Bow — see shootEagleEyeBow, which
// fires a real vanilla-style arrow entity instead of doing an instant
// hitscan+message. Still used by Mjolnir/Poseidon Trident.
func strikeRanged(p *player.Player, tx *world.Tx, target *player.Player, pos mgl64.Vec3, weaponID string, lightningChance float64) {
	if target != nil {
		target.Hurt(AttackPoints(weaponID), entity.AttackDamageSource{Attacker: p})
		p.Message(fmt.Sprintf("§b§l%s §rstrikes %s!", Defs[weaponID].DisplayName, target.Name()))
	} else {
		p.Message(fmt.Sprintf("§7%s found no target in range.", Defs[weaponID].DisplayName))
	}
	if rand.Float64() < lightningChance {
		tx.AddParticle(pos, particle.HugeExplosion{})
		tx.PlaySound(pos, sound.Explosion{})
	}
}

const arrowSpeed = 3.0 // blocks/tick — a full-power vanilla bow shot is roughly this fast

// shootEagleEyeBow fires a REAL vanilla-style arrow entity (Dragonfly's own
// built-in arrow physics/collision/damage, via entity.DefaultRegistry — not
// a custom hitscan) and consumes one real arrow from the player's
// inventory, instantly on click. Requires having an arrow; if you don't,
// nothing fires and no cooldown starts.
//
// REVERTED per explicit request back to this simple instant-fire version —
// a real draw-and-release charge mechanic (right-click to start drawing,
// release to fire with power scaling by hold time, plus a mid-air freeze
// effect while drawing) was built and shipped after this, but even once
// correctly wired to Dragonfly's real item.Releasable interface it still
// wasn't working as expected, so this reverts all of that rather than keep
// debugging it. If you want to revisit the full draw/release version
// later, it's still in this conversation's history to pull back from.
func shootEagleEyeBow(tx *world.Tx, p *player.Player, w legendaryItem, s *playerAbilityState) {
	if !hasArrow(p) {
		p.Message("§cYou need an arrow to use the Eagle Eye Bow.")
		return
	}

	p.AddEffect(effect.New(effect.JumpBoost, 3, 500*time.Millisecond))

	eye := p.Position().Add(mgl64.Vec3{0, 1.62, 0})
	vel := direction(p).Mul(arrowSpeed)
	handle := entity.DefaultRegistry.Config().Arrow(
		world.EntitySpawnOpts{Position: eye, Rotation: p.Rotation(), Velocity: vel},
		world.ArrowSpawnConfig{
			Damage:              AttackPoints("bey:eagle_eye_bow"),
			Owner:               p,
			ObtainArrowOnPickup: true,
			Tip:                 potion.Potion{},
		},
	)
	tx.AddEntity(handle)

	// Only reached if the spawn above didn't panic — safe to actually
	// take the arrow now.
	takeArrow(p)
	s.startCooldown(tx, p, w, "bey:eagle_eye_bow", 40)
}

// hasArrow reports whether p has at least one "minecraft:arrow", without
// removing anything.
func hasArrow(p *player.Player) bool {
	inv := p.Inventory()
	for slot := 0; slot < inv.Size(); slot++ {
		st, err := inv.Item(slot)
		if err != nil || st.Empty() {
			continue
		}
		name, _ := st.Item().EncodeItem()
		if name == "minecraft:arrow" {
			return true
		}
	}
	return false
}

// takeArrow removes one "minecraft:arrow" from p's inventory. Only call
// this once you're sure it's actually needed (see shootEagleEyeBow) —
// unlike hasArrow, this mutates the inventory.
func takeArrow(p *player.Player) {
	inv := p.Inventory()
	for slot := 0; slot < inv.Size(); slot++ {
		st, err := inv.Item(slot)
		if err != nil || st.Empty() {
			continue
		}
		name, _ := st.Item().EncodeItem()
		if name != "minecraft:arrow" {
			continue
		}
		inv.SetItem(slot, st.Grow(-1))
		return
	}
}

// damage immunity) for 10s (200 ticks), broken instantly by taking a hit
// (see OnHurt below) rather than by landing one — a deliberate deviation
// from the add-on's own script, per an explicit past request on this
// project. 60s (1200-tick) cooldown either way.
func shadowBladeCloak(tx *world.Tx, p *player.Player, w legendaryItem, s *playerAbilityState) {
	p.AddEffect(effect.New(effect.Invisibility, 2, 10*time.Second))
	p.AddEffect(effect.New(effect.Speed, 3, 10*time.Second))
	p.AddEffect(effect.New(effect.Resistance, 255, 10*time.Second))

	s.mu.Lock()
	s.shadowActive = true
	s.mu.Unlock()

	s.startCooldown(tx, p, w, "bey:shadow_blade", 1200)
}

// excaliburShield: Resistance 255 + a 5-hit shield charge counter, whichever
// runs out first (10s / 200 ticks either way).
func excaliburShield(tx *world.Tx, p *player.Player, w legendaryItem, s *playerAbilityState) {
	s.mu.Lock()
	s.excaliburCharges = 5
	s.excaliburUntil = time.Now().Add(10 * time.Second)
	s.mu.Unlock()

	p.AddEffect(effect.New(effect.Resistance, 255, 10*time.Second))
	s.startCooldown(tx, p, w, "bey:excalibur", 1200)
}

// dragonKatanaDash: teleports the player ~10 blocks forward and 1 block up,
// gated purely by the normal weapon cooldown (200 ticks/10s, same as
// before) — no charge requirement anymore. Simplified per explicit
// request: this used to also require 50+ accumulated "charge" (built up
// from kills) and spend 50 of it per dash; that's removed entirely now,
// along with the charge-tracking system that fed it (see OnKill below,
// which no longer grants Dragon Katana charge on kills either).
func dragonKatanaDash(tx *world.Tx, p *player.Player, w legendaryItem, s *playerAbilityState) {
	dir := direction(p)
	dest := p.Position().Add(mgl64.Vec3{dir.X() * 10, 1, dir.Z() * 10})
	p.Teleport(dest)

	s.startCooldown(tx, p, w, "bey:dragon_katana", 200)
}

// ---------------------------------------------------------------------
// On-hit / on-hurt abilities — call from PlayerHandler.HandleHurt
// ---------------------------------------------------------------------

// OnHurt applies every legendary ability that triggers when a player takes
// damage: Excalibur's shield, Shadow Blade's break-on-hit, and (for
// whoever's ATTACKING) Midas Sword's bonus damage / Crimson Chain Sword's
// Wither proc. Should be called from PlayerHandler.HandleHurt.
func OnHurt(ctx *player.Context, victim *player.Player, damage *float64, src world.DamageSource) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in OnHurt for %s: %v", victim.Name(), r)
		}
	}()

	vs := stateFor(victim.XUID())

	// --- Shadow Blade: breaks the cloak the instant you TAKE a hit ---
	vs.mu.Lock()
	wasCloaked := vs.shadowActive
	vs.shadowActive = false
	vs.mu.Unlock()
	if wasCloaked {
		victim.RemoveEffect(effect.Invisibility)
		victim.RemoveEffect(effect.Speed)
		victim.RemoveEffect(effect.Resistance)
	}

	// --- Excalibur: shield absorbs the hit entirely while charges/time remain ---
	vs.mu.Lock()
	charges := vs.excaliburCharges
	shieldUp := charges > 0 && time.Now().Before(vs.excaliburUntil)
	if shieldUp {
		vs.excaliburCharges--
	}
	remaining := vs.excaliburCharges
	vs.mu.Unlock()
	if shieldUp {
		ctx.Cancel()
		victim.Message(fmt.Sprintf("§eShield absorbed the hit! (%d left)", remaining))
		return
	}

	// --- Attacker-side on-hit abilities ---
	ads, ok := src.(entity.AttackDamageSource)
	if !ok {
		return
	}
	attacker, ok := ads.Attacker.(*player.Player)
	if !ok {
		return
	}
	held, _ := attacker.HeldItems()
	w, ok := held.Item().(legendaryItem)
	if !ok {
		return
	}
	as := stateFor(attacker.XUID())

	switch w.WeaponDef().ID {
	case "bey:midas_sword":
		// Flat bonus from the tracked power level (see MidasBonusDamage's
		// doc comment for why this isn't a real Sharpness enchant).
		*damage += MidasBonusDamage(attacker.XUID())

	case "bey:crimson_chain_sword":
		as.mu.Lock()
		ready := as.chainSwordWitherReady
		as.mu.Unlock()
		if time.Now().After(ready) {
			victim.AddEffect(effect.New(effect.Wither, 2, 3*time.Second))
			as.mu.Lock()
			as.chainSwordWitherReady = time.Now().Add(4 * time.Second)
			as.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------
// On-kill abilities — call from PlayerHandler.HandleDeath
// ---------------------------------------------------------------------

// OnKill applies Midas Sword's power growth and Crimson Chain Sword's rage
// buff when attacker kills victim with that weapon in hand. Should be
// called from PlayerHandler.HandleDeath with the attacker resolved from
// src (an entity.AttackDamageSource), if any. Player kills only — see the
// file doc comment on mob-kill coverage.
func OnKill(attacker *player.Player, victim *player.Player) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in OnKill for %s: %v", attacker.Name(), r)
		}
	}()

	held, _ := attacker.HeldItems()
	w, ok := held.Item().(legendaryItem)
	if !ok {
		return
	}

	switch w.WeaponDef().ID {
	case "bey:midas_sword":
		if rand.Intn(100) < 50 { // 50% chance per player kill
			as := stateFor(attacker.XUID())
			as.mu.Lock()
			if as.midasPower < 6 {
				as.midasPower++
			}
			power := as.midasPower
			as.mu.Unlock()
			attacker.Message(fmt.Sprintf("§6Your Midas Sword grows stronger! (Power %d)", power))
		}

	case "bey:crimson_chain_sword":
		attacker.AddEffect(effect.New(effect.Speed, 1, 4*time.Second))
		attacker.AddEffect(effect.New(effect.Strength, 1, 4*time.Second))
	}
}

// MidasBonusDamage returns the extra flat damage attacker's Midas Sword
// deals from its tracked power level (0-6, +1.25/level, same numeric curve
// as a real Sharpness enchant). Wire this into wherever Weapon's damage is
// computed, alongside AttackPoints(id).
func MidasBonusDamage(xuid string) float64 {
	s := stateFor(xuid)
	s.mu.Lock()
	defer s.mu.Unlock()
	return 1.25 * float64(s.midasPower)
}
