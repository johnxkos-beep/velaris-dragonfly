// Weapon abilities, ported from AbilityListener.php (the source plugin's
// own reverse-engineered rebuild of the Hoplite Weapons add-on's
// scripts/main.js). See the doc comments on each function below for the
// exact numbers/behavior each one is matched against.
//
// WHAT DIDN'T COME ACROSS 1:1, AND WHY:
//
//   - Mjolnir/Poseidon Trident's "throw" is an INSTANT hitscan strike (a
//     look-ray to the nearest target within range) here, not a physical
//     projectile entity that flies out, travels, and physically returns to
//     your hand over several ticks. The PHP version's "ThrownWeaponManager"
//     ticks a real flying item entity every tick for up to 35 ticks. Doing
//     that properly in Dragonfly means a custom world.Entity with its own
//     per-tick movement/collision (the same amount of work as the
//     DemonKing boss in bosses/demonking/entity.go — several hundred
//     lines). That's a reasonable follow-up if you want the weapon to
//     visually fly across the map; for now the ability still does the
//     important part (find your target, hit it, maybe strike lightning,
//     go on cooldown) instantly instead of over ~1.75s of flight.
//   - Midas Sword's growing power is tracked as an internal per-player
//     "power level" (0-6) rather than by actually writing real vanilla
//     Sharpness/Looting/Fire Aspect/Mending enchantments onto the item
//     NBT — Dragonfly's public enchantment-writing API on an item.Stack
//     wasn't confirmed against this version from the environment this was
//     written in (no network access to check). The bonus damage is the
//     same numbers Sharpness would have given; what's missing is the
//     enchant glint and the tooltip literally reading "Sharpness VI" in
//     your inventory. Tell me if you want me to wire up the real
//     enchantment next and I'll need the exact item.Stack enchant API from
//     your build to do it right instead of guessing twice.
//   - Anything the PHP version did with raw Bedrock protocol packets
//     (spoofing your held item/armor to other viewers during Shadow
//     Blade's cloak) is dropped entirely — Dragonfly's real Invisibility
//     effect (see HandleHurt below) already hides the whole player model
//     properly client-side, which is what that packet trick was working
//     around PMMP's incomplete Invisibility support to achieve. You get
//     the same result for free here.
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
	"math/rand"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// AttackPoints returns the base damage each legendary weapon deals,
// matching each *Item.php's getAttackPoints() (their real vanilla-item
// equivalent's damage). Golem Hammer=Mace(6), Midas/Shadow Blade/Emerald
// Sword/Crimson Chain Sword/Excalibur=Diamond Sword(7), Poseidon
// Trident=Trident(8), Mjolnir=Diamond Axe(9).
func AttackPoints(weaponID string) float64 {
	switch weaponID {
	case "bey:golem_hammer":
		return 6
	case "bey:mjolnir":
		return 9
	case "bey:poseidon_trident":
		return 8
	default:
		return 7
	}
}

// cooldownTicks matches each weapon's getCooldownTicks() (20 ticks/sec).
func cooldownTicks(weaponID string) int {
	switch weaponID {
	case "bey:golem_hammer":
		return 800 // 40s
	case "bey:mjolnir":
		return 900 // 45s
	case "bey:poseidon_trident":
		return 400 // 20s, shared by throw and Riptide-leap
	case "bey:shadow_blade", "bey:excalibur":
		return 1200 // 60s
	default:
		return 0 // Midas Sword/Emerald Sword/Crimson Chain Sword: passive only, no interact ability
	}
}

// ---------------------------------------------------------------------
// Per-player ability state
// ---------------------------------------------------------------------

type playerAbilityState struct {
	mu sync.Mutex

	cooldownReady map[string]time.Time // weaponID -> when it's off cooldown

	golemFallPeak     *float64  // highest Y since last touching ground, nil if grounded
	golemImmuneUntil  time.Time // fall damage waived until this time (post-leap)

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

func (s *playerAbilityState) startCooldown(weaponID string, ticks int) {
	s.mu.Lock()
	s.cooldownReady[weaponID] = time.Now().Add(time.Duration(ticks) * 50 * time.Millisecond)
	s.mu.Unlock()
}

// ---------------------------------------------------------------------
// Right-click / interact abilities — call from PlayerHandler.HandleItemUse
// ---------------------------------------------------------------------

const rangedAbilityRange = 30.0

// direction returns p's current look direction as a unit vector.
// UNVERIFIED: assumes *player.Player has a Rotation() cube.Rotation method
// (confirmed elsewhere in this repo, e.g. bosses/demonking/entity.go) and
// that cube.Rotation has a Vec3() converting yaw/pitch to a direction
// vector — the latter wasn't directly confirmed from this environment. If
// Vec3() doesn't exist on cube.Rotation, the fix is whatever the real
// method is named (likely something equivalent — yaw/pitch to direction is
// a standard conversion Dragonfly needs internally too).
func direction(p *player.Player) mgl64.Vec3 {
	return p.Rotation().Vec3()
}

// OnItemUse triggers a legendary weapon's active ability, if the player is
// holding one and it isn't on cooldown. Should be called from
// PlayerHandler.HandleItemUse, after (or alongside) knockback.OnItemUse.
//
// CAUTION (matches a documented past crash in this repo — see the comment
// on smeltedFromDrop in players/autosmelt.go): calling p.Tx() from inside
// a handler that runs on a stale/finished transaction panics with
// "world.Tx: use of transaction after transaction finishes is not
// permitted". That was confirmed specifically for the scheduled
// block-break handler; HandleItemUse fires synchronously on a normal
// interact tick, which should be safe, but this hasn't been confirmed
// against a live server. If this panics, the fix is likely to move the
// tx.PlaySound/tx.Entities() calls below out to something the caller
// passes in instead of pulling it from p.Tx() here.
func OnItemUse(ctx *player.Context, p *player.Player) {
	held, _ := p.HeldItems()
	w, ok := held.Item().(Weapon)
	if !ok {
		return
	}
	id := w.def.ID
	ticks := cooldownTicks(id)
	if ticks == 0 {
		return // Midas Sword/Emerald Sword/Crimson Chain Sword: no interact ability
	}

	s := stateFor(p.XUID())
	if s.onCooldown(id) {
		if id == "bey:poseidon_trident" {
			p.Message("§c§l[!] §r§cWeapon On Cooldown!")
		}
		return
	}

	tx := p.Tx()
	switch id {
	case "bey:golem_hammer":
		golemHammerLeap(p, s, tx)
	case "bey:mjolnir":
		throwMjolnir(p, s, tx)
	case "bey:poseidon_trident":
		throwOrRiptide(p, s, tx)
	case "bey:shadow_blade":
		shadowBladeCloak(p, s)
	case "bey:excalibur":
		excaliburShield(p, s, tx)
	}
}

// golemHammerLeap: launches the player up and forward, waives fall damage
// for 3s (60 ticks) afterward. 1.1 vertical velocity is calibrated (in the
// PHP original, against real Minecraft's velocity-decay formula, not naive
// kinematics) to land around a 7-block leap.
func golemHammerLeap(p *player.Player, s *playerAbilityState, tx *world.Tx) {
	dir := direction(p)
	p.SetVelocity(mgl64.Vec3{dir.X(), 1.1, dir.Z()})
	tx.PlaySound(p.Position(), sound.Explosion{})

	s.mu.Lock()
	s.golemImmuneUntil = time.Now().Add(3 * time.Second)
	s.mu.Unlock()

	s.startCooldown("bey:golem_hammer", 800)
}

// throwMjolnir: instant hitscan strike on the nearest look-target within
// range (see the file doc comment for why this isn't a flying projectile
// entity), always strikes lightning on the target's position, unlike
// Poseidon Trident's 35% chance.
func throwMjolnir(p *player.Player, s *playerAbilityState, tx *world.Tx) {
	target, pos := findLookTarget(p, tx, rangedAbilityRange)
	strikeRanged(p, tx, target, pos, "bey:mjolnir", 1.0)
	s.startCooldown("bey:mjolnir", 900)
}

// throwOrRiptide: sneaking + using launches a Riptide-style forward leap
// (no water required, matching the PHP fix — the add-on's real script
// doesn't gate this on water either). Standing + using throws the trident
// (35% lightning chance) instead. Both share the same 400-tick (20s)
// cooldown.
func throwOrRiptide(p *player.Player, s *playerAbilityState, tx *world.Tx) {
	if p.Sneaking() {
		dir := direction(p)
		p.SetVelocity(mgl64.Vec3{dir.X() * 8, 1.2, dir.Z() * 8})
		tx.PlaySound(p.Position(), sound.Explosion{})
		s.startCooldown("bey:poseidon_trident", 400)
		return
	}
	target, pos := findLookTarget(p, tx, rangedAbilityRange)
	strikeRanged(p, tx, target, pos, "bey:poseidon_trident", 0.35)
	s.startCooldown("bey:poseidon_trident", 400)
}

// strikeRanged hits target (if found) for the weapon's base damage, and
// rolls lightningChance to also strike lightning at pos.
func strikeRanged(p *player.Player, tx *world.Tx, target *player.Player, pos mgl64.Vec3, weaponID string, lightningChance float64) {
	if target != nil {
		target.Hurt(AttackPoints(weaponID), entity.AttackDamageSource{Attacker: p})
	}
	if rand.Float64() < lightningChance {
		tx.AddParticle(pos, particle.HugeExplosion{})
		tx.PlaySound(pos, sound.Explosion{})
		// UNVERIFIED: a real lightning-bolt entity spawn (visual bolt +
		// vanilla lightning damage/fire) needs Dragonfly's lightning
		// entity constructor, which wasn't confirmed from this
		// environment. Standing in for it with an explosion flash/sound
		// at the impact point for now — cosmetically different, but the
		// weapon's own direct-hit damage above already lands regardless.
	}
}

// shadowBladeCloak: Invisibility II, Speed III, Resistance 255 (near-total
// damage immunity) for 10s (200 ticks), broken instantly by taking a hit
// (see OnHurt below) rather than by landing one — a deliberate deviation
// from the add-on's own script, per an explicit past request on this
// project. 60s (1200-tick) cooldown either way.
func shadowBladeCloak(p *player.Player, s *playerAbilityState) {
	p.AddEffect(effect.New(effect.Invisibility, 2, 10*time.Second))
	p.AddEffect(effect.New(effect.Speed, 3, 10*time.Second))
	p.AddEffect(effect.New(effect.Resistance, 255, 10*time.Second))

	s.mu.Lock()
	s.shadowActive = true
	s.mu.Unlock()

	s.startCooldown("bey:shadow_blade", 1200)
}

// excaliburShield: Resistance 255 + a 5-hit shield charge counter, whichever
// runs out first (10s / 200 ticks either way). The item's own lore text
// says "3 incoming hits" — that's a stale label carried over from the
// original add-on's UI, not what it actually does; 5 is correct (see
// AbilityListener.php's own comment on $excaliburCharges).
func excaliburShield(p *player.Player, s *playerAbilityState, tx *world.Tx) {
	s.mu.Lock()
	s.excaliburCharges = 5
	s.excaliburUntil = time.Now().Add(10 * time.Second)
	s.mu.Unlock()

	p.AddEffect(effect.New(effect.Resistance, 255, 10*time.Second))
	tx.PlaySound(p.Position(), sound.Explosion{})
	s.startCooldown("bey:excalibur", 1200)
}

// findLookTarget scans nearby entities for the closest Player within a
// ~25-degree cone of where p is looking, within range blocks. Returns the
// player found (or nil) and the point to strike at (either the target's
// position or the max range point along the look direction, matching the
// PHP original's endPoint fallback for lightning-at-nothing).
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

// ---------------------------------------------------------------------
// On-hit / on-hurt abilities — call from PlayerHandler.HandleHurt
// ---------------------------------------------------------------------

// OnHurt applies every legendary ability that triggers when a player takes
// damage: Golem Hammer's post-leap fall immunity, Excalibur's shield,
// Shadow Blade's break-on-hit, and (for whoever's ATTACKING) Golem
// Hammer's fall-strike bonus / Emerald Sword's sharpness / Crimson Chain
// Sword's Wither proc / Midas Sword's kill-tracking hit record. Should be
// called from PlayerHandler.HandleHurt, before the knockback package's own
// fall-damage halving (order doesn't matter between the two — they touch
// different damage causes/paths) or after; either is fine.
func OnHurt(ctx *player.Context, victim *player.Player, damage *float64, src world.DamageSource) {
	vs := stateFor(victim.XUID())

	// --- Fall damage: Golem Hammer post-leap immunity ---
	if _, ok := src.(entity.FallDamageSource); ok {
		vs.mu.Lock()
		immune := time.Now().Before(vs.golemImmuneUntil)
		vs.mu.Unlock()
		if immune {
			ctx.Cancel()
			return
		}
	}

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
	w, ok := held.Item().(Weapon)
	if !ok {
		return
	}
	as := stateFor(attacker.XUID())

	switch w.def.ID {
	case "bey:golem_hammer":
		as.mu.Lock()
		peak := as.golemFallPeak
		as.golemFallPeak = nil
		as.mu.Unlock()
		if peak != nil {
			fallDistance := *peak - attacker.Position().Y()
			if fallDistance > 0.5 {
				*damage += fallDistance * 1.5
				groundSlam(attacker, victim.Position(), attacker.Tx())
			}
		}

	case "bey:midas_sword":
		// Flat bonus from the tracked power level (see MidasBonusDamage's
		// doc comment for why this isn't a real Sharpness enchant).
		*damage += MidasBonusDamage(attacker.XUID())

	case "bey:emerald_sword":
		sharpness := countEmeralds(attacker) / 3
		if sharpness > 10 {
			sharpness = 10
		}
		if sharpness > 0 {
			*damage += sharpnessBonusDamage(*damage, sharpness)
		}

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

// groundSlam: Mace-style AOE knockback burst around a Golem Hammer
// fall-strike's impact point. Not present in the decompiled add-on script
// — added because "work as a mace" was explicitly requested for this
// weapon in the past.
func groundSlam(attacker *player.Player, impact mgl64.Vec3, tx *world.Tx) {
	tx.AddParticle(impact, particle.HugeExplosion{})
	tx.PlaySound(impact, sound.Explosion{})

	for e := range tx.Entities() {
		other, ok := e.(*player.Player)
		if !ok || other == attacker {
			continue
		}
		push := other.Position().Sub(impact)
		dist := push.Len()
		if dist < 0.5 {
			dist = 0.5
		}
		if dist > 3.5 {
			continue
		}
		strength := 0.6 * (1 - dist/3.5)
		hl := mgl64.Vec3{push.X(), 0, push.Z()}.Len()
		if hl < 0.1 {
			hl = 0.1
		}
		other.SetVelocity(mgl64.Vec3{
			(push.X() / hl) * strength,
			0.4 * (1 - dist/3.5),
			(push.Z() / hl) * strength,
		})
	}
}

// sharpnessBonusDamage is the real vanilla Sharpness damage formula
// (armor-aware; toughness treated as 0, same simplification the PHP
// original used, since neither PMMP nor this port track Bedrock's
// toughness stat). Ported directly from AbilityListener::sharpnessBonusDamage.
func sharpnessBonusDamage(baseDamage float64, level int) float64 {
	// Armor isn't threaded through from OnHurt's signature (Dragonfly's
	// damage pipeline applies armor reduction after this hook runs, not
	// before), so this computes the flat, pre-armor Sharpness bonus
	// (1.25 per level) rather than the full armor-interaction formula the
	// PHP version used. Slightly overstates the bonus against heavily
	// armored targets; everyone else sees the same numbers either way.
	return 1.25 * float64(level)
}

func countEmeralds(p *player.Player) int {
	count := 0
	inv := p.Inventory()
	for slot := 0; slot < inv.Size(); slot++ {
		st, err := inv.Item(slot)
		if err != nil || st.Empty() {
			continue
		}
		name, _ := st.Item().EncodeItem()
		switch name {
		case "minecraft:emerald":
			count += st.Count()
		case "minecraft:emerald_block":
			count += st.Count() * 9
		}
	}
	return count
}

// ---------------------------------------------------------------------
// Golem Hammer fall tracking — call once/tick per online player, e.g. from
// a lightweight ticker goroutine or an existing per-tick hook in main.go.
// ---------------------------------------------------------------------

// TickGolemFall tracks p's highest Y since they last touched ground, while
// they're holding the Golem Hammer — mirrors AbilityListener::tickGolemFall.
// Call this once a tick (20/sec) for every online player.
func TickGolemFall(p *player.Player) {
	held, _ := p.HeldItems()
	w, ok := held.Item().(Weapon)
	if !ok || w.def.ID != "bey:golem_hammer" {
		return
	}
	s := stateFor(p.XUID())
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.OnGround() {
		s.golemFallPeak = nil
		return
	}
	y := p.Position().Y()
	if s.golemFallPeak == nil || y > *s.golemFallPeak {
		s.golemFallPeak = &y
	}
}

// ---------------------------------------------------------------------
// On-kill abilities — call from PlayerHandler.HandleDeath
// ---------------------------------------------------------------------

// OnKill applies Midas Sword's power growth and Crimson Chain Sword's rage
// buff when attacker kills victim with that weapon in hand. Should be
// called from PlayerHandler.HandleDeath with the attacker resolved from
// src (an entity.AttackDamageSource), if any.
func OnKill(attacker *player.Player, victim *player.Player) {
	held, _ := attacker.HeldItems()
	w, ok := held.Item().(Weapon)
	if !ok {
		return
	}

	switch w.def.ID {
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
// as a real Sharpness enchant — see the file doc comment for why this is
// tracked as an internal counter instead of a real enchantment). Wire this
// into wherever Weapon's damage is computed, alongside AttackPoints(id).
func MidasBonusDamage(xuid string) float64 {
	s := stateFor(xuid)
	s.mu.Lock()
	defer s.mu.Unlock()
	return 1.25 * float64(s.midasPower)
}
