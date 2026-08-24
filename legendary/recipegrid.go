package legendary

// GridSlot is one of the 9 ingredient-grid slots on the "Craft Weapon"
// screen. Both fields empty means an unused/blank slot.
type GridSlot struct {
	ItemID    string // raw vanilla item id shown as the button's text, e.g. "minecraft:diamond"
	TextureID string // resource-pack texture path (no extension), or "" for a blank slot
}

// recipeGrids is the 3x3 "Craft Weapon" ingredient grid for each legendary
// weapon, keyed by weapon ID — see form.go's sendCraftGrid for why this
// exists and the exact 12-button layout it's used to build. 6 of the 8
// entries below (everything except Dragon Katana and Eagle Eye Bow) are
// copied 1:1 from the original PHP plugin's own RecipeGridData.php, which
// itself was copied 1:1 from the add-on's real recipeData table — same
// items, same slot positions, same texture paths. Golem Hammer's and
// Emerald Sword's grids existed in that source too but are dropped here
// since those two weapons were removed from the roster.
//
// Dragon Katana's and Eagle Eye Bow's grids are NEW — the original PHP
// plugin never had these two weapons, so there's no source grid to copy.
// Laid out to match the same visual convention as every other entry here
// (single-count ingredients on the top/bottom edges, a double-count
// ingredient split across the left/right edges, the "base tool" ingredient
// centered), using each weapon's real Recipe from items.go for what
// materials/counts to place. Texture paths follow the same two
// conventions everything else here uses: "textures/recipe-icons/<name>"
// for the handful of materials the add-on's own resource pack ships an
// enlarged icon for (confirmed against the pack's actual file listing),
// and "textures/items/<name>" (a vanilla, always-available Bedrock path,
// not something this resource pack needs to ship) for everything else.
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
		{"minecraft:iron_block", "textures/recipe-icons/iron_block"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond_axe", "textures/items/diamond_axe"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:gold_block", "textures/recipe-icons/gold_block"},
		{"minecraft:diamond", "textures/items/diamond"},
	},
	"bey:poseidon_trident": {
		{"minecraft:seagrass", "textures/recipe-icons/seagrass"},
		{"minecraft:nautilus_shell", "textures/items/nautilus"},
		{"minecraft:seagrass", "textures/recipe-icons/seagrass"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:trident", "textures/items/trident"},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:seagrass", "textures/recipe-icons/seagrass"},
		{"minecraft:water_bucket", "textures/recipe-icons/water_bucket"},
		{"minecraft:seagrass", "textures/recipe-icons/seagrass"},
	},
	"bey:shadow_blade": {
		{"minecraft:coal_block", "textures/recipe-icons/coal_block"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:coal_block", "textures/recipe-icons/coal_block"},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:gold_ingot", "textures/items/gold_ingot"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
	},
	"bey:crimson_chain_sword": {
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:chain", "textures/items/chain"},
		{"minecraft:activator_rail", "textures/blocks/rail_activator"},
		{"minecraft:diamond_block", "textures/recipe-icons/diamond_block"},
		{"minecraft:redstone", "textures/items/redstone_dust"},
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
	// NEW — see the doc comment above.
	"bey:dragon_katana": {
		{},
		{"minecraft:fire_charge", "textures/recipe-icons/fire_charge"},
		{},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{"minecraft:diamond_sword", "textures/items/diamond_sword"},
		{"minecraft:obsidian", "textures/recipe-icons/obsidian"},
		{},
		{"minecraft:netherite_scrap", "textures/items/netherite_scrap"},
		{},
	},
	"bey:eagle_eye_bow": {
		{},
		{"minecraft:rabbit_foot", "textures/items/rabbit_foot"},
		{},
		{"minecraft:diamond", "textures/items/diamond"},
		{"minecraft:bow", "textures/items/bow_standby"},
		{"minecraft:diamond", "textures/items/diamond"},
		{},
		{"minecraft:glowstone", "textures/recipe-icons/glow_stone"},
		{},
	},
}
