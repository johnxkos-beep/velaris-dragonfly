package legendary

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// ---------------------------------------------------------------------
// /legendary — codex menu (browse all 8, pick one for detail/craft)
// ---------------------------------------------------------------------

// codexMenu is the top-level "browse all legendaries" form. Each button's
// position matches Order, so Submit can look the weapon back up by index.
type codexMenu struct {
	buttons []form.Button
}

func (m codexMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b == pressed {
			sendWeaponDetail(p, Order[i])
			return
		}
	}
}

func sendCodexForm(p *player.Player) {
	buttons := make([]form.Button, 0, len(Order))
	for _, id := range Order {
		d := Defs[id]
		label := d.DisplayName
		if Mgr.HasClaimed(id) {
			label += fmt.Sprintf("\n§7(claimed by %s)", Mgr.ClaimedBy(id))
		}
		buttons = append(buttons, form.NewButton(label, iconPath(id)))
	}
	menu := form.NewMenu(codexMenu{buttons: buttons}, "Legendary Codex").
		WithBody("Select a weapon to view its lore and recipe.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// iconPath is the form-button icon path for a weapon.
//
// UNVERIFIED: this was the missing piece behind the codex menu showing
// blank/textless icon slots (form.NewButton's 2nd argument was previously
// always "" — plain oversight, not an API mismatch). "textures/items/%s"
// (item shortname, e.g. "textures/items/midas_sword") is my best guess at
// the path Dragonfly's auto-generated resource pack registers a
// world.CustomItem's icon under — this format matches vanilla Bedrock's
// own item_texture.json convention, which Dragonfly is very likely
// mirroring, but I couldn't confirm the exact generated path from this
// environment. If the codex still shows blank icons after this, the
// button text itself will still work fine — tell me and I'll try the
// alternate path Dragonfly actually uses.
func iconPath(id string) string {
	return fmt.Sprintf("textures/items/%s", shortID(id))
}

// ---------------------------------------------------------------------
// Weapon detail form — lore, recipe, and a Craft button.
// ---------------------------------------------------------------------

type weaponDetailMenu struct {
	weaponID string
	craft    form.Button // zero value if not currently craftable (already claimed)
	back     form.Button
}

func (m weaponDetailMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if pressed == m.back {
		sendCodexForm(p)
		return
	}
	if m.craft != (form.Button{}) && pressed == m.craft {
		if err := Mgr.Craft(p, m.weaponID); err != nil {
			p.Message("§c" + err.Error())
			return
		}
		d := Defs[m.weaponID]
		p.Message(fmt.Sprintf("§aYou crafted the §l%s§r§a!", d.DisplayName))
		// Matches the original plugin's server-wide broadcast on every
		// legendary craft (Server::broadcastMessage in LegendaryManager.php).
		announcement := fmt.Sprintf("§6§l\"%s\" §r§6has crafted \"%s\".", p.Name(), d.DisplayName)
		for other := range state.Server.Players(tx) {
			other.Message(announcement)
		}
	}
}

func sendWeaponDetail(p *player.Player, weaponID string) {
	d, ok := Defs[weaponID]
	if !ok {
		return
	}

	body := ""
	for _, line := range d.Lore {
		body += line + "\n"
	}
	body += "\n§6Recipe: §r" + DescribeRecipe(d)

	back := form.NewButton("Back", "")
	m := weaponDetailMenu{weaponID: weaponID, back: back}

	if Mgr.HasClaimed(weaponID) {
		body += fmt.Sprintf("\n\n§7Already claimed by %s.", Mgr.ClaimedBy(weaponID))
		p.SendForm(form.NewMenu(m, d.DisplayName).WithBody(body).WithButtons(back))
		return
	}

	craft := form.NewButton("Craft", iconPath(weaponID))
	m.craft = craft
	if !HasIngredients(p, d) {
		body += "\n\n§cYou're missing ingredients."
	}
	p.SendForm(form.NewMenu(m, d.DisplayName).WithBody(body).WithButtons(craft, back))
}
