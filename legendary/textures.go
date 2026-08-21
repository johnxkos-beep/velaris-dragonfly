package legendary

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	_ "image/png"
)

// assets embeds the 8 weapon icon PNGs pulled directly out of the
// HopliteV1.mcpack resource pack you sent (textures/weapon/<id>.png in the
// pack). Dragonfly builds its client resource pack from these automatically
// via world.CustomItem.Texture() — there's nothing else to install.
//
//go:embed assets/*.png
var assets embed.FS

// loadTexture decodes assets/<shortID>.png. Panics on startup if a texture
// is missing, since a legendary registered without an icon would silently
// show the client's missing-texture placeholder — exactly the bug the
// original plugin's README spent a whole section fixing. Better to fail
// loudly at boot than ship that bug again.
func loadTexture(shortID string) image.Image {
	b, err := assets.ReadFile(fmt.Sprintf("assets/%s.png", shortID))
	if err != nil {
		panic(fmt.Sprintf("legendary: missing embedded texture for %q: %v", shortID, err))
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		panic(fmt.Sprintf("legendary: failed to decode texture for %q: %v", shortID, err))
	}
	return img
}
