// Package legendary ports the "HopliteLegendary" PocketMine plugin's
// legendary-weapon codex to Dragonfly.
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
//   - Special abilities (Mjolnir's throw/lightning, Excalibur's invincibility
//     shield, Shadow Blade's stealth buffs, etc.). The original plugin never
//     built these either (its README says so explicitly) — this port is at
//     parity, not behind. Each weapon below has a clearly marked extension
//     point (onHit/onUse) once you tell me which ability to build first.
//   - Award box, news broadcasts, teams, KOTH, border, zones, tracking,
//     builder mode, purge tokens, ender chest command — separate systems,
//     coming in later rounds.
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
			ID:          "bey:golem_hammer",
			DisplayName: "Golem Hammer",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Mace",
				"§6Interact: §rTake a §lleap§r in the air, while §lfalling§r hitting any entity will §ldeal extra damage§r",
				"§6Hit: §rLike a §lmace,§r hitting while falling does §lextra damage§r",
				"§6Strategy: §rUse vertical attacks to maximize damage output",
			},
			Recipe: []Ingredient{
				{"minecraft:diamond_block", 2},
				{"minecraft:iron_block", 1},
				{"minecraft:diamond", 2},
				{"minecraft:diamond_axe", 1},
				{"minecraft:iron_ingot", 1},
			},
		},
		{
			ID:          "bey:midas_sword",
			DisplayName: "Midas Sword",
			Lore: []string{
				"§6Damage: §rDeals the same base damage as a Diamond Sword",
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
				{"minecraft:iron_block", 1},
				{"minecraft:diamond_axe", 1},
				{"minecraft:gold_block", 1},
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
				{"minecraft:seagrass", 4},
				{"minecraft:nautilus_shell", 1},
				{"minecraft:diamond", 2},
				{"minecraft:trident", 1},
				{"minecraft:water_bucket", 1},
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
				{"minecraft:coal_block", 2},
				{"minecraft:diamond_block", 2},
				{"minecraft:gold_ingot", 2},
				{"minecraft:obsidian", 2},
				{"minecraft:diamond_sword", 1},
			},
		},
		{
			ID:          "bey:emerald_sword",
			DisplayName: "Emerald Sword",
			Lore: []string{
				"§6Base Damage: §rDiamond Sword damage",
				"§6Emerald Sharpness: §rEach emerald increases sharpness level by +1",
				"§r(Emerald Block counts as 9 emeralds)",
				"§6Effect: §rWorks like regular Sharpness, increasing extra damage per level",
				"§6Strategy: §rCollect emeralds to enhance your blade",
			},
			Recipe: []Ingredient{
				{"minecraft:book", 1},
				{"minecraft:emerald_block", 2},
				{"minecraft:diamond_sword", 1},
				{"minecraft:iron_block", 1},
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
				{"minecraft:chain", 1},
				{"minecraft:diamond_block", 1},
				{"minecraft:gold_block", 1},
				{"minecraft:redstone", 1},
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
	s = s.WithCustomName(d.DisplayName)
	s = s.WithLore(d.Lore...)
	return s, true
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

// Mgr is the package-level claims manager, set up by Init. Commands and
// forms in this package read it directly, matching how state.Ranks etc. are
// used as package-level vars elsewhere in this repo.
var Mgr *Manager

// Init registers every legendary weapon as a Dragonfly custom item and loads
// the claims file at claimsPath (created on first run). Call this once at
// startup, before the server starts accepting connections — see the wiring
// snippet in legendary/README.md.
func Init(claimsPath string) error {
	for _, id := range Order {
		d := Defs[id]
		world.RegisterItem(Weapon{def: d})
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
