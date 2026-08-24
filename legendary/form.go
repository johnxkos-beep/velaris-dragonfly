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
	// Title must be exactly "Hoplite Codex Weapons" — the shipped resource
	// pack's ui/server_form.json only swaps in the styled panel/background
	// (title_background.png, body_background.png, etc.) when the form's
	// title text matches one of 4 exact strings it checks for. "Legendary
	// Codex" (what this said before) matched none of them, so the client
	// silently fell back to the plain default form chrome — that's the
	// whole bug behind the codex looking unstyled in your screenshot, not
	// a missing-pack or Dragonfly-forms problem.
	menu := form.NewMenu(codexMenu{buttons: buttons}, "Hoplite Codex Weapons").
		WithBody("Select a weapon to view its lore and recipe.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// iconPath is the form-button icon path for a weapon, pulled from the real
// pack's textures/item_texture.json (e.g. "bey_golem_hammer" ->
// "textures/icons/golem_hammer") — fixes a wrong guess from an earlier
// pass ("textures/items/%s", which doesn't exist in the pack at all, so
// every codex button showed no icon).
func iconPath(id string) string {
	return fmt.Sprintf("textures/icons/%s", shortID(id))
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

	// Same reasoning as sendCodexForm's title: the shipped pack's UI skin
	// only kicks in on an exact title match. "Weapon Details" is what it
	// checks for the view-only (already-claimed) screen.
	if Mgr.HasClaimed(weaponID) {
		body += fmt.Sprintf("\n\n§7Already claimed by %s.", Mgr.ClaimedBy(weaponID))
		p.SendForm(form.NewMenu(m, "Weapon Details").WithBody(body).WithButtons(back))
		return
	}

	craft := form.NewButton("Craft", iconPath(weaponID))
	m.craft = craft
	if !HasIngredients(p, d) {
		body += "\n\n§cYou're missing ingredients."
	}
	// "Hoplite Codex Craft" is what the pack's UI skin checks for the
	// craftable screen (its internal label still reads "Craft Weapon" —
	// that text comes from the pack itself, not this title string).
	p.SendForm(form.NewMenu(m, "Hoplite Codex Craft").WithBody(body).WithButtons(craft, back))
}
