package demonking

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed textures/demon_king_trophy.png
var trophyIconBytes []byte

// Trophy is a special item dropped only by the Demon King on death — it
// has no crafting recipe, no other source, and (deliberately) no
// UsableOnBlock/other behaviour, so it's purely a "you beat the boss"
// keepsake. Identifier "tnt:lord_demon_trophy".
type Trophy struct{}

func (Trophy) EncodeItem() (name string, meta int16) {
	return "tnt:lord_demon_trophy", 0
}

func (Trophy) Name() string { return "§5Demon King's Core" }

func (Trophy) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(trophyIconBytes))
	if err != nil {
		panic("demonking: failed to decode trophy icon: " + err.Error())
	}
	return img
}

func (Trophy) Category() category.Category { return category.Items() }

// MaxCount limits the trophy to a single stack slot per drop — purely
// cosmetic, doesn't affect the "unobtainable elsewhere" part.
func (Trophy) MaxCount() int { return 1 }

var _ world.CustomItem = Trophy{}
