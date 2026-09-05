package enderdragon

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed textures/dragon_egg.png
var eggIconBytes []byte

// Egg is the Dragon Egg dropped when the Ender Dragon dies. Unlike the
// dragon/crystal entities (which use real vanilla identifiers because
// there's no risk of colliding with an entity Dragonfly doesn't ship), this
// is a CUSTOM item with its own identifier and a placeholder icon — same
// pattern as demonking.Trophy — rather than reusing "minecraft:dragon_egg",
// since Dragonfly may already auto-register a vanilla dragon egg BLOCK's
// item counterpart internally, and there's no way to check that without a
// live build. Swap in real Dragon Egg block art later if you want the
// pixel-perfect vanilla look; this only needs to work as a keepsake, not be
// placeable.
type Egg struct{}

func (Egg) EncodeItem() (name string, meta int16) { return "velaris:dragon_egg", 0 }
func (Egg) Name() string                          { return "§5Dragon Egg" }

func (Egg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(eggIconBytes))
	if err != nil {
		panic("enderdragon: failed to decode dragon egg icon: " + err.Error())
	}
	return img
}

func (Egg) Category() category.Category { return category.Items() }

// MaxCount limits it to a single stack slot, purely cosmetic — matches
// vanilla (dragon eggs don't stack either).
func (Egg) MaxCount() int { return 1 }

var _ world.CustomItem = Egg{}
