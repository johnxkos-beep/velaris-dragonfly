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
// UPDATED to match items.go's real Recipe lists after several rounds of
// changes: Poseidon Trident's underwater-only ingredients (seagrass,
// nautilus shell) were swapped for ore/mineral items per explicit
// request, and 7 of the 8 weapons had one common ingredient replaced with
// the Demon King's Core (bosses/demonking's boss-only drop, real
// identifier "tnt:lord_demon_trophy") and another with a Netherite
// Ingot. Dragon Katana is the deliberate exception — it uses neither,
// requiring a Dragon Egg instead (see items.go's Dragon Katana Recipe for
// why).
//
// COSMETIC GAP: the Demon King's Core has no icon in this grid's texture
// set — it isn't a vanilla item (so no "textures/items/..." path exists
// for it) and isn't part of the add-on's own resource pack either (it's
// this repo's own item, added long after that pack was built). Standing
// in with a vanilla nether-star icon below since it's a reasonable
// "rare boss material" visual stand-in; the actual crafting check in
// manager.go only cares about the real item identifier matching
// (tnt:lord_demon_trophy), completely independent of what icon shows
// here, so this is purely cosmetic and doesn't affect whether crafting
// actually works. Netherite Ingot is a real vanilla item, so its icon
// path ("textures/items/netherite_ingot") should be correct as-is.
var recipeGrids = map[string][9]GridSlot{
	"bey:midas_sword": {
		{},
		{"minecraft:enchanted_golden_apple", "textures/recipe-icons/enchanted_golden_apple"},
		{},
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
		{},
		{},
	},
	"bey:mjolnir": {
		{"minecraft:diamond", "textures/items/diamond"},
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
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
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:trident", "textures/items/trident"},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
	},
	"bey:shadow_blade": {
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
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
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{"minecraft:gold_block", "textures/recipe-icons/gold_block"},
	},
	"bey:excalibur": {
		{},
		{"minecraft:enchanted_golden_apple", "textures/recipe-icons/enchanted_golden_apple"},
		{},
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{},
	},
	"bey:dragon_katana": {
		{},
		{"minecraft:dragon_egg", "textures/blocks/dragon_egg"},
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
		{"tnt:lord_demon_trophy", "textures/items/nether_star"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:bow", "textures/items/bow_standby"},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
		{"minecraft:netherite_ingot", "textures/items/netherite_ingot"},
		{},
	},
}
