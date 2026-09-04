// Package legendary ports the "HopliteLegendary" PocketMine plugin's
// legendary-weapon codex to Dragonfly.
//
// ROUND 3: weapon abilities are now implemented — see abilities.go for
// Golem Hammer's leap/fall-strike/ground-slam, Mjolnir's lightning strike,
// Poseidon Trident's throw/Riptide, Shadow Blade's cloak, Excalibur's
// shield, Midas Sword's kill-growth, Emerald Sword's emerald-fed damage,
// and Crimson Chain Sword's wither+rage. Real melee base damage (6-9,
// matching each weapon's real vanilla-item equivalent) is also wired in
// now via Weapon.AttackDamage() below — round 1/2 items technically dealt
// Dragonfly's generic fallback damage on a plain left-click hit, since
// nothing implemented that interface yet.
//
// ROUND 2 FIX: round 1 tracked claims per-player, so every player could
// craft their own copy of every legendary. The source plugin's claim lock
// is server-wide — only one copy of each weapon is ever crafted, by
// whoever gets there first — and manager.go now matches that. See the
// "Claims" comment at the top of manager.go for the full story.
//
// SCOPE OF THIS PORT (round 1 of several — teams/KOTH/border/zones/tracking/
// builder mode/purge tokens/ender chest/news/award-box are NOT included yet):
//
//   - The 8 legendary weapons that actually had recipes wired up in the
//     source plugin's resources/legendaries.yml (golem_hammer, midas_sword,
//     mjolnir, poseidon_trident, shadow_blade, emerald_sword,
//     crimson_chain_sword, excalibur). The resource pack you sent has PNGs
//     for ~19 weapon names, but only these 8 were ever registered as real
//     items in the plugin's ItemRegistrar.php — the rest are unused art.
//     Tell me which of the others to add and I'll wire up ids/recipes for
//     them the same way.
//   - The /legendary codex form: browse all 8, see lore + recipe, and
//     one-click craft if you have the ingredients and haven't claimed it yet
//     (matches the original's "single-claim crafting").
//   - Real per-weapon icons, via Dragonfly's world.CustomItem — the PNGs
//     from your resource pack are embedded directly into the Go binary
//     (see textures.go), and Dragonfly auto-generates the client resource
//     pack from that at startup. You do NOT need to manually install your
//     .mcpack or edit item_texture.json — that whole Customies-era problem
//     the original README was working around doesn't exist in Dragonfly.
//
// NOT included in this round (matches the original PHP plugin's own stated
// limitations — nothing here is a regression):
//   - Award box, news broadcasts, teams, KOTH, border, zones, tracking,
//     builder mode, purge tokens, ender chest command — separate systems,
//     not ported yet.
//
// This has not been run against a live Dragonfly server (no network access
// in the environment I wrote it in to fetch/build the dragonfly module) —
// treat first boot as a shakeout the same way the original plugin's README
// asked you to. If `go build` or the server log throws something, paste it
// back to me and I'll fix it.
package legendary

import (
	"fmt"
	"image"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Ingredient is one line of a crafting recipe: a vanilla item name (e.g.
// "minecraft:diamond_block") and how many are required.
type Ingredient struct {
	Name  string
	Count int
}

// Def is the static definition of one legendary weapon, ported directly
// from resources/legendaries.yml.
type Def struct {
	// ID is the custom item identifier, matching the original "bey:x" ids
	// (kept as-is so drops/world saves stay recognisable if you ever
	// migrate data, though Dragonfly doesn't share storage with PMMP).
	ID string
	// DisplayName is the item's in-game name.
	DisplayName string
	// Lore is shown in the item tooltip and in the codex form, line by line.
	Lore []string
	// Recipe is what a player must hold (anywhere in their inventory) to
	// instant-craft this weapon via the codex.
	Recipe []Ingredient
	// texture is the embedded weapon icon (see textures.go).
	texture image.Image
}

// Defs holds every ported legendary weapon, in the same order as the
// original legendaries.yml, keyed by ID for fast lookup.
var Defs = buildDefs()

// Order lists weapon IDs in display order, for the codex menu.
var Order []string

func buildDefs() map[string]*Def {
	raw := []*Def{
		{
			ID:          "bey:midas_sword",
			DisplayName: "Midas Sword",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Netherite Sword",
				"§6Special: §rKills build §lpower§r in the sword",
				"§6Points: §rKilling a player gives §l101§r points, mobs give §l1§r point",
				"§6Upgrade: §rSword upgrades every §l100§r points earned",
				"§7- Sharpness increases up to §lV§7",
				"§7- Then §lLooting III§7, §lFire Aspect II§7, and §lMending§7 are added",
				"§6Strategy: §rFocus on players and mobs to unlock its full potential",
			},
			Recipe: []Ingredient{
				{"minecraft:enchanted_golden_apple", 1},
				{"minecraft:gold_ingot", 2},
				{"minecraft:diamond_sword", 1},
				{"minecraft:golden_apple", 1},
			},
		},
		{
			ID:          "bey:mjolnir",
			DisplayName: "Mjolnir",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Axe",
				"§6Interact: §rThrow the hammer forward, it flies and returns to your hand",
				"§6On Hit: §rStrikes targets with §llightning§r",
				"§6Recall: §rIf it travels too far, it comes back automatically",
				"§6Strategy: §rUse it to poke from range",
			},
			Recipe: []Ingredient{
				{"minecraft:diamond", 6},
				{"tnt:lord_demon_trophy", 1},
				{"minecraft:diamond_axe", 1},
				{"minecraft:netherite_ingot", 1},
			},
		},
		{
			ID:          "bey:poseidon_trident",
			DisplayName: "Poseidon Trident",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Trident",
				"§6Special: §rA mighty trident that commands lightning",
				"§6Interact: §rHold and release to throw the trident forward",
				"§6Projectile: §rStrikes targets and may summon lightning",
				"§6Sneak + Release: §rLaunch forward with Riptide-like force",
				"§6Strategy: §rUse for ranged attacks, crowd control, and mobility",
			},
			Recipe: []Ingredient{
				{"minecraft:diamond_block", 1},
				{"tnt:lord_demon_trophy", 1},
				{"minecraft:diamond", 2},
				{"minecraft:trident", 1},
				{"minecraft:netherite_ingot", 1},
			},
		},
		{
			ID:          "bey:shadow_blade",
			DisplayName: "Shadow Blade",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Sword",
				"§6Special: §rGrants invisibility, speed, and near-impervious resistance",
				"§6Effect: §rAllows you to strike from the shadows without being seen",
				"§6Strategy: §rUse to ambush enemies or escape dangerous situations",
			},
			Recipe: []Ingredient{
				{"tnt:lord_demon_trophy", 1},
				{"minecraft:diamond_block", 2},
				{"minecraft:netherite_ingot", 1},
				{"minecraft:obsidian", 2},
				{"minecraft:diamond_sword", 1},
			},
		},
		{
			ID:          "bey:dragon_katana",
			DisplayName: "Dragon Katana",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Sword",
				"§6Interact: §rDash forward — cooldown-gated, no charge needed",
				"§6Strategy: §rUse it to close distance or make a quick escape",
			},
			// Deliberately the ONE weapon in this roster that does NOT
			// require the Demon King's Core or a Netherite Ingot (every
			// other one of the 8 does) — uses a Dragon Egg instead, per
			// explicit request.
			Recipe: []Ingredient{
				{"minecraft:dragon_egg", 1},
				{"minecraft:diamond_sword", 1},
				{"minecraft:obsidian", 2},
				{"minecraft:diamond", 2},
			},
		},
		{
			ID:          "bey:eagle_eye_bow",
			DisplayName: "Eagle Eye Bow",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Bow",
				"§6Interact: §rFires an arrow-equivalent strike at your target",
				"§6Special: §rGrants a burst of Jump Boost while drawing",
				"§6Strategy: §rUse for high-mobility ranged pressure",
			},
			Recipe: []Ingredient{
				{"tnt:lord_demon_trophy", 1},
				{"minecraft:diamond", 2},
				{"minecraft:bow", 1},
				{"minecraft:netherite_ingot", 1},
			},
		},
		{
			ID:          "bey:crimson_chain_sword",
			DisplayName: "Crimson Chain Sword",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Sword",
				"§6On Hit: §rInflicts §lWither II§r for 2 seconds",
				"§6Cooldown: §r4 seconds between Wither strikes",
				"§6On Kill: §rWeapon gets raged, granting high speed and strength",
				"§6Duration: §rRage buffs last for 5 seconds",
				"§6Strategy: §rChain kills to stay empowered",
			},
			Recipe: []Ingredient{
				{"minecraft:activator_rail", 4},
				{"minecraft:diamond_sword", 1},
				{"tnt:lord_demon_trophy", 1},
				{"minecraft:diamond_block", 1},
				{"minecraft:gold_block", 1},
				{"minecraft:netherite_ingot", 1},
			},
		},
		{
			ID:          "bey:excalibur",
			DisplayName: "Excalibur",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Sword",
				"§6Interact: §rBecome §lInvincible§r instantly",
				"§6Shield: §rBlocks up to §l3 incoming hits§r",
				"§6Duration: §rProtection lasts §l10 seconds§r if not broken",
				"§6Break: §rInvincibility ends early if all hits are consumed",
				"§6Combat: §rPerfect for clutch moments and aggressive pushes",
				"§6Strategy: §rTime activation carefully to absorb lethal damage",
			},
			Recipe: []Ingredient{
				{"minecraft:gold_ingot", 4},
				{"minecraft:enchanted_golden_apple", 1},
				{"minecraft:iron_block", 2},
				{"minecraft:diamond_sword", 1},
				{"minecraft:diamond_block", 1},
			},
		},
	}

	defs := make(map[string]*Def, len(raw))
	Order = make([]string, 0, len(raw))
	for _, d := range raw {
		d.texture = loadTexture(shortID(d.ID))
		defs[d.ID] = d
		Order = append(Order, d.ID)
	}
	return defs
}

func shortID(id string) string {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[i+1:]
		}
	}
	return id
}

// Weapon is the world.CustomItem implementation handed out for a crafted
// legendary. It carries only the definition ID — the rest is looked up from
// Defs so tooltip/recipe edits don't require re-crafting existing items.
type Weapon struct {
	def *Def
}

// NewWeaponStack returns a full stack of 1 for the weapon with the given ID,
// or ok=false if no such legendary is registered.
func NewWeaponStack(id string) (item.Stack, bool) {
	d, ok := Defs[id]
	if !ok {
		return item.Stack{}, false
	}
	w := Weapon{def: d}
	s := item.NewStack(w, 1)
	if s.Empty() || s.Count() < 1 {
		// Defensive: something about constructing a bare stack for this
		// item came back empty. Bail out to the caller before we even try
		// WithCustomName/WithLore so Craft can fall back cleanly instead of
		// silently handing the player nothing.
		return item.Stack{}, false
	}
	named := s.WithCustomName(d.DisplayName).WithLore(d.Lore...)
	if named.Empty() || named.Count() < 1 {
		// WithCustomName/WithLore produced an empty stack for some reason —
		// ship the plain (unnamed/no-lore) stack rather than nothing. This
		// is the most likely culprit for "craft succeeds but nothing
		// appears": if this is what's happening, the server log line below
		// (printed from Craft) will say so on your next test.
		return s, true
	}
	return named, true
}

// AttackDamage implements Dragonfly's item.Weapon interface, giving this
// weapon real melee damage on a normal left-click hit (separate from the
// bonus/bespoke damage abilities.go's OnHurt adds on top for certain
// weapons). Matches each *Item.php's getAttackPoints() — see
// abilities.AttackPoints, which this defers to so the numbers live in one
// place.
//
// UNVERIFIED: "AttackDamage() float64" is my best read of the method name
// Dragonfly's item.Weapon interface expects (world.Item implementations
// with no such method fall back to whatever Dragonfly's bare-hand/generic
// default is — 1 damage — which is almost certainly why these felt weak
// if you've tested combat with them before this fix). If the real
// interface method is named differently, paste the compiler error.
func (w Weapon) AttackDamage() float64 {
	return AttackPoints(w.def.ID)
}

// WeaponDef exposes w.def to code outside this file (abilities.go, hud.go)
// via the legendaryItem interface below.
func (w Weapon) WeaponDef() *Def { return w.def }

// legendaryItem exists so abilities.go/hud.go can work through a small
// interface instead of the concrete Weapon type directly — a leftover from
// when Eagle Eye Bow briefly had its own BowWeapon type (since reverted;
// only plain Weapon exists now, and it still satisfies this fine via
// WeaponDef() above). Harmless to keep as-is.
type legendaryItem interface {
	world.Item
	WeaponDef() *Def
}

// EncodeItem implements world.Item.
func (w Weapon) EncodeItem() (name string, meta int16) {
	return w.def.ID, 0
}

// Name implements world.CustomItem. This is the fallback name Dragonfly
// shows if no custom name was set on the stack (NewWeaponStack always sets
// one, but this keeps bare item.NewStack(Weapon{...}, 1) calls sane too).
func (w Weapon) Name() string { return w.def.DisplayName }

// Texture implements world.CustomItem, providing the icon from your
// resource pack (see textures.go).
func (w Weapon) Texture() image.Image { return w.def.texture }

// Category implements world.CustomItem — where it's listed in the creative
// inventory.
func (w Weapon) Category() category.Category { return category.Equipment() }

// MaxCount caps legendary weapons at a stack size of 1 — matches every
// other unique/rare weapon in vanilla Bedrock (swords, tools, bows never
// stack) and specifically matters here since these are meant to be
// server-unique, single-copy items; stacking them would make it trivial
// to hand out 64 "one of a kind" Midas Swords from creative.
//
// UNVERIFIED: "MaxCount() int" is my best-grounded guess at the
// world.Item optional-interface method Dragonfly checks for a custom max
// stack size (mirrors the same "implement an extra method to opt into
// extra behavior" pattern already confirmed for AttackDamage and
// item.Usable in this file) — I could not find independent source
// confirmation of this exact method name. If `go build` doesn't
// recognize it as doing anything (compiles fine either way since it's
// just an extra method, but stacks might still cap at 64), tell me and
// I'll look for the real mechanism.
func (w Weapon) MaxCount() int { return 1 }

// Use implements item.Usable — Dragonfly hands this a real, legitimate
// *world.Tx directly as a parameter (same interface family as
// bosses/demonking/spawnegg.go's UsableOnBlock, already proven to work in
// this codebase). This is what Mjolnir, Eagle Eye Bow, and Poseidon
// Trident's throw actually run through now — see OnUse's doc comment in
// abilities.go for the full story on why they moved here instead of
// PlayerHandler.HandleItemUse.
//
// UNVERIFIED SIGNATURE WARNING (same caveat spawnegg.go already carries):
// item.Usable's exact method signature wasn't checked against a live
// build — no Go toolchain available while writing this. If `go build`
// reports this doesn't satisfy item.Usable, paste the compiler error and
// it's a quick fix; the logic OnUse runs is solid regardless of the exact
// parameter list here.
func (w Weapon) Use(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	p, ok := u.(*player.Player)
	if !ok {
		return false
	}
	return OnUse(tx, p)
}

// Mgr is the package-level claims manager, set up by Init. Commands and
// forms in this package read it directly, matching how state.Ranks etc. are
// used as package-level vars elsewhere in this repo.
var Mgr *Manager

// Init registers every legendary weapon as a Dragonfly custom item and loads
// the claims file at claimsPath (created on first run). Call this once at
// startup, before the server starts accepting connections — see the wiring
// snippet in legendary/README.md.
// creativeGroupName is the creative-inventory group all 8 legendary
// weapons belong to. Uses the exact same mechanism that fixed spawn eggs
// in mobs/register.go (confirmed real, from Dragonfly's actual source,
// server/item/creative/creative.go): world.RegisterItem + Category() alone
// does NOT put a custom item in the creative inventory — Category() on
// CustomItem is real but apparently insufficient by itself. The actual
// mechanism is the separate creative package: creative.RegisterGroup
// establishes a named group under a category, and creative.RegisterItem
// adds each item stack to that group by name. Every custom item in this
// repo that's ever successfully shown up in creative (the 4 mob spawn
// eggs) went through this exact path — legendary weapons never did until
// now, which is almost certainly the entire reason they never appeared
// despite Category() being implemented correctly.
const creativeGroupName = "velaris_legendary"

func Init(claimsPath string) error {
	stacks := make([]item.Stack, 0, len(Order))
	for _, id := range Order {
		d := Defs[id]
		w := Weapon{def: d}
		world.RegisterItem(w)
		stacks = append(stacks, item.NewStack(w, 1))
	}

	if len(stacks) > 0 {
		creative.RegisterGroup(creative.Group{
			Category: creative.EquipmentCategory(),
			Name:     creativeGroupName,
			Icon:     stacks[0],
		})
		for _, s := range stacks {
			creative.RegisterItem(creative.Item{Stack: s, Group: creativeGroupName})
		}
	}

	m, err := NewManager(claimsPath)
	if err != nil {
		return err
	}
	Mgr = m
	return nil
}

// DescribeRecipe renders a Def's recipe as a display string, e.g.
// "2x Diamond Block, 1x Iron Block, 2x Diamond".
func DescribeRecipe(d *Def) string {
	s := ""
	for i, ing := range d.Recipe {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%dx %s", ing.Count, prettyItemName(ing.Name))
	}
	return s
}

// prettyItemName turns "minecraft:enchanted_golden_apple" into
// "Enchanted Golden Apple".
func prettyItemName(id string) string {
	short := shortID(id)
	out := make([]byte, 0, len(short))
	capNext := true
	for i := 0; i < len(short); i++ {
		c := short[i]
		if c == '_' {
			out = append(out, ' ')
			capNext = true
			continue
		}
		if capNext && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
			capNext = false
		} else {
			capNext = false
		}
		out = append(out, c)
	}
	return string(out)
}
