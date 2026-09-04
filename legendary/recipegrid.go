package legendary

// GridSlot is one of the 9 ingredient-grid slots on the "Craft Weapon"
// screen. Both fields empty means an unused/blank slot.
type GridSlot struct {
	ItemID    string // raw vanilla item id shown as the button's text, e.g. "minecraft:diamond"
	TextureID string // resource-pack texture path (no extension), or "" for a blank slot
}

// recipeGrids is the 3x3 "Craft Weapon" ingredient grid for each legendary
// weapon, keyed by weapon ID — see form.go's sendCraftGrid for why this
// exists and the exact 12-button layout it's used to build.
//
// Midas Sword and Excalibur are back to their ORIGINAL recipes (and grids)
// per explicit request — the Demon King's Core / Netherite Ingot swap from
// an earlier round didn't apply to these two after all. 6 of the other 7
// weapons still have that swap; Dragon Katana never had it (uses a Dragon
// Egg instead, see items.go).
//
// ICON PATHS, ROUND 2: the Demon King's Core previously used a vanilla
// nether-star icon as an explicitly-flagged placeholder (it isn't a
// vanilla item, so no real "textures/items/..." path was confirmed for
// it). Switched to "textures/items/lord_demon_trophy" — a guess following
// the same short-identifier convention already used throughout this file
// for every other custom item icon in this codebase (matches the
// approach in form.go's iconPath), using the real trophy item's actual
// identifier (tnt:lord_demon_trophy, see bosses/demonking/trophy.go) minus
// its namespace. Still not independently confirmed — if it renders wrong
// again, that specific string is the one thing to try alternatives for.
// Dragon Egg's path also changed from "textures/blocks/dragon_egg" (which
// rendered incorrectly per a real screenshot) to "textures/items/dragon_egg"
// — Bedrock sometimes files a block-as-item's icon under "items" rather
// than "blocks" depending on how it's mapped, so this is the other most
// likely path for the same real texture.
var recipeGrids = map[string][9]GridSlot{
	"bey:midas_sword": {
		{},
		{"minecraft:enchanted_golden_apple", "textures/recipe-icons/enchanted_golden_apple"},
		{},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{},
		{"minecraft:golden_apple", "textures/items/apple_golden"},
		{},
	},
	"bey:mjolnir": {
		{"minecraft:diamond", "textures/items/diamond"},
		{"tnt:lord_demon_trophy", "textures/items/lord_demon_trophy"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond_axe", "textures/items/diamond_axe"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{"minecraft:diamond", "textures/items/diamond"},
	},
	"bey:poseidon_trident": {
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"tnt:lord_demon_trophy", "textures/items/lord_demon_trophy"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:trident", "textures/items/trident"},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
	},
	"bey:shadow_blade": {
		{"tnt:lord_demon_trophy", "textures/items/lord_demon_trophy"},
		{},
		{},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
	},
	"bey:crimson_chain_sword": {
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"tnt:lord_demon_trophy", "textures/items/lord_demon_trophy"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{"minecraft:gold_block", "textures/recipe-icons/gold_block"},
	},
	"bey:excalibur": {
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:enchanted_golden_apple", "textures/recipe-icons/enchanted_golden_apple"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:iron_block", "textures/recipe-icons/iron_block"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:iron_block", "textures/recipe-icons/iron_block"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
	},
	"bey:dragon_katana": {
		{},
		{"minecraft:dragon_egg", "textures/items/dragon_egg"},
		{},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
	},
	"bey:eagle_eye_bow": {
		{},
		{"tnt:lord_demon_trophy", "textures/items/lord_demon_trophy"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:bow", "textures/items/bow_standby"},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
	},
}
