package players

import (
	"encoding/json"
	"os"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
)

// AutoSmeltConfig controls which ores are auto-smelted on break, and
// whether AutoSmeltPermissionCheck is consulted before smelting. Ported
// from the original PocketMine AutoSmeltOre plugin (YTBJero) — field names
// match the original config.yml keys for continuity.
type AutoSmeltConfig struct {
	RequirePermission bool `json:"permission"`
	IronOre           bool `json:"Iron_Ore"`
	DeepslateIronOre  bool `json:"Deepslate_Iron_Ore"`
	GoldOre           bool `json:"Gold_Ore"`
	DeepslateGoldOre  bool `json:"Deepslate_Gold_Ore"`
	AncientDebris     bool `json:"Ancient_Debris"`
	// NetherQuartzOre exists for config-file compatibility with the
	// original plugin. The original never actually wired up a smelted
	// result for quartz either — this toggle was a no-op there too.
	NetherQuartzOre bool `json:"Nether_Quartz_Ore"`
}

// autoSmeltConfig is the active AutoSmeltOre configuration. Defaults match
// the original plugin's stock config.yml (everything on, permission
// required).
var autoSmeltConfig = AutoSmeltConfig{
	RequirePermission: true,
	IronOre:           true,
	DeepslateIronOre:  true,
	GoldOre:           true,
	DeepslateGoldOre:  true,
	AncientDebris:     true,
	NetherQuartzOre:   true,
}

// AutoSmeltPermissionCheck is consulted before smelting whenever
// RequirePermission is true. Wire this up from main() to whatever
// rank/permission logic Velaris uses, e.g.:
//
//	players.AutoSmeltPermissionCheck = func(p *player.Player) bool {
//	    return rks.Of(p.XUID()) != ranks.DefaultRankName
//	}
//
// If left nil, RequirePermission is effectively ignored and every player
// is allowed to auto-smelt (this matches the original plugin's default
// behaviour, where the permission node was granted to everyone by default).
var AutoSmeltPermissionCheck func(p *player.Player) bool

// LoadAutoSmeltConfig loads the AutoSmeltOre config from the JSON file at
// path. If the file doesn't exist, it's created with the defaults above.
// Call this once from main() before srv.Accept(), the same way you'd load
// ranks or ops.
func LoadAutoSmeltConfig(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b, err = json.MarshalIndent(autoSmeltConfig, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, b, 0644)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &autoSmeltConfig)
}

// smeltedOreDrop returns the smelted item.Stack for the given block ID
// (e.g. "minecraft:iron_ore"), and whether that ore is currently enabled
// in the config. Deepslate variants smelt to the same ingot as their
// stone counterparts, matching vanilla behaviour.
func smeltedOreDrop(blockID string) (item.Stack, bool) {
	switch blockID {
	case "minecraft:iron_ore":
		return item.NewStack(item.IronIngot{}, 1), autoSmeltConfig.IronOre
	case "minecraft:deepslate_iron_ore":
		return item.NewStack(item.IronIngot{}, 1), autoSmeltConfig.DeepslateIronOre
	case "minecraft:gold_ore":
		return item.NewStack(item.GoldIngot{}, 1), autoSmeltConfig.GoldOre
	case "minecraft:deepslate_gold_ore":
		return item.NewStack(item.GoldIngot{}, 1), autoSmeltConfig.DeepslateGoldOre
	case "minecraft:ancient_debris":
		return item.NewStack(item.NetheriteScrap{}, 1), autoSmeltConfig.AncientDebris
	}
	return item.Stack{}, false
}

// HandleBlockBreak auto-smelts eligible ores into their ingot form when
// broken without Silk Touch. Mirrors the original AutoSmeltOre plugin.
func (h *PlayerHandler) HandleBlockBreak(ctx *player.Context, pos cube.Pos, drops *[]item.Stack, xp *int) {
	if autoSmeltConfig.RequirePermission && AutoSmeltPermissionCheck != nil && !AutoSmeltPermissionCheck(h.p) {
		return
	}

	mainHand, _ := h.p.HeldItems()
	if _, silk := mainHand.Enchantment(enchantment.SilkTouch); silk {
		// Silk Touch: let the default drops (the ore block itself) stand.
		return
	}

	id, _ := h.p.Tx().Block(pos).EncodeBlock()
	smelted, enabled := smeltedOreDrop(id)
	h.log.Info("autosmelt: block broken", "id", id, "matched", enabled, "config_permission_required", autoSmeltConfig.RequirePermission)
	if !enabled {
		return
	}
	*drops = []item.Stack{smelted}
}
