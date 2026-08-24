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
//   - Mjolnir/Poseidon Trident's "throw" and Eagle Eye Bow's "shoot" are
//     NOT ranged strikes anymore as of this pass — they PRIME your next
//     successful melee hit with the weapon's bonus effect instead (see
//     primeNextHit and the "Consume a primed ranged-strike bonus" block in
//     OnHurt below). The first version of this file did an instant
//     "scan nearby entities, hit whoever you're looking at" on right-click,
//     which needed a *world.Tx fetched via p.Tx() inside HandleItemUse —
//     that's not safe to call there in this Dragonfly version (confirmed
//     by a real crash log: it panicked on every single use, not just at
//     disconnect, meaning the fetch itself was the problem, not a rare
//     race). The one other safe-Tx pattern already proven to work
//     elsewhere in this repo (players/autosmelt.go's HandleBlockBreak fix)
//     is to avoid Tx() entirely and work only with what's handed to you as
//     a parameter — HandleAttackEntity/HandleHurt hand you the actual
//     target directly, no scanning required, so that's what these three
//     lean on now. If you want true at-range strikes (hit someone you're
//     just looking at, not standing next to), that needs either a real
//     projectile entity (its own Tick(tx) gets a legitimate Tx from
//     Dragonfly's own scheduler — same scale of work as the DemonKing
//     boss) or confirmation from your actual Dragonfly source of how
//     player.Context safely exposes the current Tx, if it does at all.
//   - The "lightning strike" visual/sound effects (particle + sound at the
//     impact point) are dropped for the same reason — they went through
//     tx.AddParticle/tx.PlaySound, which needs the same unsafe Tx. Wither
//     is used as a rough damage-over-time stand-in for "got struck by
//     lightning" instead.
//   - Dragon Katana's charge tracking is an internal per-player counter
//     (0-100, same as the add-on's own scripting-API version, which reads
//     the point count back out of the item's lore text every tick) rather
//     than literally storing it in the item's lore/name — same tradeoff as
//     Midas Sword's power level below. The dash itself (teleport ~10
//     blocks forward+up, cost 50 charge, "mob.endermen.portal" sound) is a
//     direct match.
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
// NOT COVERED: mob kills toward Dragon Katana's charge (+1/mob in the
// add-on, vs +50/player) and Midas Sword's mob-kill point path. This repo
// currently only has a death hook for PLAYER deaths (PlayerHandler.HandleDeath
// in players/players.go) — there's no mob-death hook wired up anywhere yet
// for either weapon to listen on. Player kills (the main path for both)
// work fully; mob kills don't add charge/points until a mob-death hook
// exists. Tell me if you want that added.
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
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// AttackPoints returns the base damage each legendary weapon deals,
// matching each item's real vanilla-item equivalent's damage, straight
// from the add-on's own item component data ("minecraft:damage").
// Dragon Katana/Midas/Shadow Blade/Crimson Chain Sword/Excalibur=Diamond
// Sword(7)... except the add-on's own file actually defines Dragon Katana
// at 8, matching Poseidon Trident, not 7 — kept as 8 to match the source
// data exactly rather than the "Diamond Sword" lore line, which is stale
// flavor text in the original too. Eagle Eye Bow=Bow(3). Mjolnir=Diamond
// Axe(9).
func AttackPoints(weaponID string) float64 {
	switch weaponID {
	case "bey:dragon_katana", "bey:poseidon_trident":
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
		return 200 // 10s dash cooldown, but only spendable at 50+ charge
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

	dragonKatanaCharge int // 0-100, +50/player kill, dash at 50+ costs 50

	primedWeapon string    // weapon ID whose next-hit bonus is armed, or ""
	primedUntil  time.Time // primed bonus expires if not used by this time
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

// direction returns p's current look direction as a unit vector.
// UNVERIFIED: assumes *player.Player has a Rotation() cube.Rotation method
// (confirmed elsewhere in this repo, e.g. bosses/demonking/entity.go) and
// that cube.Rotation has a Vec3() converting yaw/pitch to a direction
// vector — the latter wasn't directly confirmed from this environment.
func direction(p *player.Player) mgl64.Vec3 {
	return p.Rotation().Vec3()
}

// OnItemUse triggers a legendary weapon's active ability, if the player is
// holding one and it isn't on cooldown. Should be called from
// PlayerHandler.HandleItemUse, after (or alongside) knockback.OnItemUse.
//
// RECOVERS FROM PANICS (added after a real crash report): a ranged ability
// hitting a player who disconnects at the exact same instant can panic
// with "world.Tx: use of transaction after transaction finishes is not
// permitted" — Dragonfly routes a hit on another player through a
// cross-transaction call (entity_ref.go's CallRef/awaitTask), and that
// call can lose the race against the target's own transaction tearing
// down mid-quit. That's an edge case in how two different players'
// transactions get bridged, not a bug in the ability logic itself, and
// it's rare enough (has to land on the exact tick someone disconnects)
// that crashing the whole server over it is the wrong tradeoff — so this
// now recovers, logs it, and drops just that one interaction instead.
func OnItemUse(ctx *player.Context, p *player.Player) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in OnItemUse for %s: %v", p.Name(), r)
		}
	}()

	held, _ := p.HeldItems()
	w, ok := held.Item().(Weapon)
	if !ok {
		return
	}
	id := w.def.ID
	ticks := cooldownTicks(id)
	if ticks == 0 {
		return // Midas Sword/Crimson Chain Sword: no interact ability
	}

	s := stateFor(p.XUID())
	if s.onCooldown(id) {
		if id == "bey:poseidon_trident" {
			p.Message("§c§l[!] §r§cWeapon On Cooldown!")
		}
		return
	}

	switch id {
	case "bey:mjolnir":
		primeNextHit(p, s, "bey:mjolnir", 900)
	case "bey:poseidon_trident":
		throwOrRiptide(p, s)
	case "bey:shadow_blade":
		shadowBladeCloak(p, s)
	case "bey:excalibur":
		excaliburShield(p, s)
	case "bey:dragon_katana":
		dragonKatanaDash(p, s)
	case "bey:eagle_eye_bow":
		eagleEyeShoot(p, s)
	}
}

// primeNextHit arms weaponID's ranged-strike bonus so it applies to
// attacker's very next successful melee hit (consumed in OnHurt below)
// instead of scanning for a live target right now. See the file doc
// comment for why this replaced an instant hitscan-at-range design.
func primeNextHit(p *player.Player, s *playerAbilityState, weaponID string, cooldown int) {
	s.mu.Lock()
	s.primedWeapon = weaponID
	s.primedUntil = time.Now().Add(5 * time.Second)
	s.mu.Unlock()
	p.Message("§b§lCharged! §rYour next hit is empowered.")
	s.startCooldown(weaponID, cooldown)
}

// throwOrRiptide: sneaking + using launches a Riptide-style forward leap
// (no water required — the add-on's own script doesn't gate this on water
// either). Standing + using primes the trident's next-hit bonus (35%
// lightning chance) instead. Both share the same 400-tick (20s) cooldown.
func throwOrRiptide(p *player.Player, s *playerAbilityState) {
	if p.Sneaking() {
		dir := direction(p)
		p.SetVelocity(mgl64.Vec3{dir.X() * 8, 1.2, dir.Z() * 8})
		s.startCooldown("bey:poseidon_trident", 400)
		return
	}
	primeNextHit(p, s, "bey:poseidon_trident", 400)
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
// runs out first (10s / 200 ticks either way).
func excaliburShield(p *player.Player, s *playerAbilityState) {
	s.mu.Lock()
	s.excaliburCharges = 5
	s.excaliburUntil = time.Now().Add(10 * time.Second)
	s.mu.Unlock()

	p.AddEffect(effect.New(effect.Resistance, 255, 10*time.Second))
	s.startCooldown("bey:excalibur", 1200)
}

// dragonKatanaDash: at 50+ charge, teleports the player ~10 blocks forward
// and 1 block up, and spends 50 charge. Below 50 charge, tells the player
// they don't have enough and does nothing else (no cooldown spent).
func dragonKatanaDash(p *player.Player, s *playerAbilityState) {
	s.mu.Lock()
	charge := s.dragonKatanaCharge
	s.mu.Unlock()
	if charge < 50 {
		p.Message("§c§l[!]§r You dont have enough charge.")
		return
	}

	dir := direction(p)
	dest := p.Position().Add(mgl64.Vec3{dir.X() * 10, 1, dir.Z() * 10})
	p.Teleport(dest)

	s.mu.Lock()
	s.dragonKatanaCharge -= 50
	s.mu.Unlock()

	s.startCooldown("bey:dragon_katana", 200)
}

// eagleEyeShoot: Jump Boost burst (matching the add-on's "drawing the bow"
// buff) plus priming the bow's next-hit bonus damage — see the file doc
// comment for why this isn't a real flying arrow with headshot detection.
func eagleEyeShoot(p *player.Player, s *playerAbilityState) {
	// UNVERIFIED: "effect.JumpBoost" is my best guess at Dragonfly's name
	// for this effect type. If it's named differently in your build, it's
	// a one-line fix.
	p.AddEffect(effect.New(effect.JumpBoost, 3, 500*time.Millisecond))
	primeNextHit(p, s, "bey:eagle_eye_bow", 40)
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
	w, ok := held.Item().(Weapon)
	if !ok {
		return
	}
	as := stateFor(attacker.XUID())

	// --- Consume a primed ranged-strike bonus (Mjolnir/Poseidon
	// Trident/Eagle Eye Bow), if this weapon armed one and it hasn't
	// expired. This is what actually delivers those 3 weapons' "ranged"
	// bonus now — see primeNextHit's doc comment in the interact-ability
	// section above for why it's on-next-hit instead of an instant scan at
	// use time (that needed a *world.Tx that isn't safely available from
	// HandleItemUse in this Dragonfly version — confirmed by a real crash,
	// not a guess this time).
	as.mu.Lock()
	primed := as.primedWeapon == w.def.ID && time.Now().Before(as.primedUntil)
	if primed {
		as.primedWeapon = ""
	}
	as.mu.Unlock()
	if primed {
		switch w.def.ID {
		case "bey:mjolnir":
			victim.AddEffect(effect.New(effect.Wither, 1, 2*time.Second)) // stand-in for "always strikes lightning"
			attacker.Message("§b§lMjolnir's charge strikes true!")
		case "bey:poseidon_trident":
			if rand.Float64() < 0.35 {
				victim.AddEffect(effect.New(effect.Wither, 1, 2*time.Second))
				attacker.Message("§b§lThe trident's charge strikes true!")
			}
		case "bey:eagle_eye_bow":
			*damage += 2 // simplified stand-in for the real headshot/draw-power bonus
		}
	}

	switch w.def.ID {
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

// OnKill applies Midas Sword's power growth, Crimson Chain Sword's rage
// buff, and Dragon Katana's charge growth when attacker kills victim with
// that weapon in hand. Should be called from PlayerHandler.HandleDeath
// with the attacker resolved from src (an entity.AttackDamageSource), if
// any. Player kills only — see the file doc comment on mob-kill coverage.
func OnKill(attacker *player.Player, victim *player.Player) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in OnKill for %s: %v", attacker.Name(), r)
		}
	}()

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

	case "bey:dragon_katana":
		as := stateFor(attacker.XUID())
		as.mu.Lock()
		as.dragonKatanaCharge += 50
		if as.dragonKatanaCharge > 100 {
			as.dragonKatanaCharge = 100
		}
		charge := as.dragonKatanaCharge
		as.mu.Unlock()
		attacker.Message(fmt.Sprintf("§bDragon Katana charge: %d/100", charge))
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
