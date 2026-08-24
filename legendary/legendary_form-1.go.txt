package legendary

import (
	"fmt"
	"log"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// iconPath is the form-button icon path for a weapon, pulled from the real
// pack's textures/item_texture.json (e.g. "bey_golem_hammer" ->
// "textures/icons/golem_hammer").
func iconPath(id string) string {
	return fmt.Sprintf("textures/icons/%s", shortID(id))
}

// weaponIconPath is the BIG 3D-style weapon icon shown on the "Craft
// Weapon" recipe grid screen (slot index 9) — a different, larger image
// than iconPath's codex-list icon. Matches the original PHP plugin's
// Util::weaponIconPathFor.
func weaponIconPath(id string) string {
	return fmt.Sprintf("textures/recipe-weapon-icon/%s", shortID(id))
}

// ---------------------------------------------------------------------
// /legendary — main menu: "Craft Weapons" / "Weapons and Details" /
// (op-only) "Reset All Legendaries". Matches the original PHP plugin's
// LegendaryCommand::openMainMenu 1:1, including which title strings
// trigger the shipped resource pack's custom UI skin and which don't.
// ---------------------------------------------------------------------

type mainMenu struct {
	craft   form.Button
	details form.Button
	reset   form.Button // zero value if the player isn't op
}

func (m mainMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed {
	case m.craft:
		sendCraftList(p)
	case m.details:
		sendDetailsList(p)
	default:
		if m.reset != (form.Button{}) && pressed == m.reset {
			sendResetConfirm(p)
		}
	}
}

// sendMainMenu opens the top-level /legendary menu. Call this from
// Command.Run instead of jumping straight to a weapon list.
func sendMainMenu(p *player.Player) {
	craft := form.NewButton("Craft Weapons", "")
	details := form.NewButton("Weapons and Details", "")
	m := mainMenu{craft: craft, details: details}
	buttons := []form.Button{craft, details}

	if state.Ops.IsOp(p.XUID()) {
		reset := form.NewButton("§cReset All Legendaries", "")
		m.reset = reset
		buttons = append(buttons, reset)
	}

	// Title must be exactly "Hoplite Codex" for the pack's UI skin to
	// apply to this screen.
	p.SendForm(form.NewMenu(m, "Hoplite Codex").WithButtons(buttons...))
}

// ---------------------------------------------------------------------
// "Craft Weapons" tab — only unclaimed weapons; picking one either crafts
// instantly (if you already have every ingredient) or opens the recipe
// grid.
// ---------------------------------------------------------------------

type craftListMenu struct {
	buttons []form.Button
	ids     []string // parallel to buttons — only the unclaimed weapon IDs
}

func (m craftListMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b == pressed {
			handleCraftSelection(p, m.ids[i])
			return
		}
	}
}

func sendCraftList(p *player.Player) {
	var buttons []form.Button
	var ids []string
	for _, id := range Order {
		if Mgr.HasClaimed(id) {
			continue
		}
		buttons = append(buttons, form.NewButton(Defs[id].DisplayName, iconPath(id)))
		ids = append(ids, id)
	}

	if len(buttons) == 0 {
		// Matches the original's MessageForm fallback when every legendary
		// is already claimed — a single-button menu stands in for that here.
		ok := form.NewButton("OK", "")
		p.SendForm(form.NewMenu(okOnly{ok: ok}, "Hoplite Codex Weapons").
			WithBody("§7Every legendary weapon has already been claimed on this server.").
			WithButtons(ok))
		return
	}

	p.SendForm(form.NewMenu(craftListMenu{buttons: buttons, ids: ids}, "Hoplite Codex Weapons").
		WithButtons(buttons...))
}

// handleCraftSelection matches LegendaryCommand::handleCraftSelection: skip
// the recipe screen entirely and craft instantly if the player already has
// everything.
func handleCraftSelection(p *player.Player, weaponID string) {
	d, ok := Defs[weaponID]
	if !ok {
		return
	}
	if HasIngredients(p, d) {
		if err := Mgr.Craft(p, weaponID); err != nil {
			p.Message("§c" + err.Error())
			return
		}
		announceCraft(p, d)
		return
	}
	sendCraftGrid(p, weaponID)
}

func announceCraft(p *player.Player, d *Def) {
	p.Message(fmt.Sprintf("§aCrafted %s successfully!", d.DisplayName))
}

// okOnly is a trivial single-button acknowledgement menu.
type okOnly struct{ ok form.Button }

func (okOnly) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {}

// ---------------------------------------------------------------------
// "Craft Weapon" recipe grid — THE fix for the blank-grid bug. The shipped
// resource pack reskins any form titled exactly "Hoplite Codex Craft" into
// a 3x3 ingredient grid + a big 3D weapon icon + two craft buttons, by
// binding a fixed 12-slot button collection in a hardcoded order (see
// recipegrid.go for the full breakdown of why and the exact layout).
// Sending anything other than exactly 12 buttons in that order — which is
// what the previous 2-button (Craft/Back) version of this screen did —
// still gets the grid BACKGROUND (since that swap is keyed off the title
// alone) but leaves every slot with nothing to bind to, hence blank. This
// is what your screenshot showed.
// ---------------------------------------------------------------------

type craftGridMenu struct {
	weaponID    string
	craftButton form.Button // index 10 — the only button that does anything
}

func (m craftGridMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if pressed != m.craftButton {
		return // every other slot (grid decorations, the icon, the decorative 2nd craft button) is a no-op, matching the original
	}
	attemptCraft(p, m.weaponID, tx)
}

func sendCraftGrid(p *player.Player, weaponID string) {
	d, ok := Defs[weaponID]
	if !ok {
		return
	}
	grid, ok := recipeGrids[weaponID]
	if !ok {
		// No grid art for this weapon — fall back to the old text-list
		// screen rather than show a broken/blank grid.
		sendCraftTextFallback(p, weaponID)
		return
	}

	buttons := make([]form.Button, 0, 12)
	for _, slot := range grid { // indices 0-8: the 9 recipe grid slots
		buttons = append(buttons, form.NewButton(slot.ItemID, slot.TextureID))
	}
	buttons = append(buttons, form.NewButton("", weaponIconPath(weaponID))) // index 9: big 3D weapon icon
	craftLabel := "Craft " + d.DisplayName
	craftButton := form.NewButton(craftLabel, "") // index 10: the actual craft trigger
	buttons = append(buttons, craftButton)
	buttons = append(buttons, form.NewButton(craftLabel, "textures/ui/prev_page")) // index 11: decorative 2nd button, matches the add-on 1:1

	m := craftGridMenu{weaponID: weaponID, craftButton: craftButton}
	// Title must be exactly "Hoplite Codex Craft".
	p.SendForm(form.NewMenu(m, "Hoplite Codex Craft").WithButtons(buttons...))
}

func attemptCraft(p *player.Player, weaponID string, tx *world.Tx) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in attemptCraft for %s: %v", p.Name(), r)
		}
	}()

	d, ok := Defs[weaponID]
	if !ok {
		return
	}
	if Mgr.HasClaimed(weaponID) {
		p.Message(fmt.Sprintf("§c%s has already been claimed.", d.DisplayName))
		return
	}
	if err := Mgr.Craft(p, weaponID); err != nil {
		p.Message("§c" + err.Error())
		return
	}
	announceCraft(p, d)
	// Matches the original plugin's server-wide broadcast on every
	// legendary craft (Server::broadcastMessage in LegendaryManager.php).
	broadcast := fmt.Sprintf("§6§l\"%s\" §r§6has crafted \"%s\".", p.Name(), d.DisplayName)
	for other := range state.Server.Players(tx) {
		other.Message(broadcast)
	}
}

// sendCraftTextFallback: plain-text recipe list, used only if a weapon
// somehow has no recipeGrids entry (shouldn't normally happen — every
// current weapon has one).
func sendCraftTextFallback(p *player.Player, weaponID string) {
	d, ok := Defs[weaponID]
	if !ok {
		return
	}
	body := "§lRecipe for " + d.DisplayName + "§r\n\n" + DescribeRecipe(d)
	if !HasIngredients(p, d) {
		body += "\n\n§7You're missing some materials — come back with the rest to craft this instantly."
	}
	ok2 := form.NewButton("OK", "")
	p.SendForm(form.NewMenu(okOnly{ok: ok2}, d.DisplayName+" - Recipe").WithBody(body).WithButtons(ok2))
}

// ---------------------------------------------------------------------
// "Weapons and Details" tab — every weapon (claimed or not), picking one
// shows lore only, no craft option.
// ---------------------------------------------------------------------

type detailsListMenu struct {
	buttons []form.Button
}

func (m detailsListMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b == pressed {
			sendLoreOnly(p, Order[i])
			return
		}
	}
}

func sendDetailsList(p *player.Player) {
	buttons := make([]form.Button, 0, len(Order))
	for _, id := range Order {
		label := Defs[id].DisplayName
		if Mgr.HasClaimed(id) {
			label += " §7(claimed)"
		}
		buttons = append(buttons, form.NewButton(label, iconPath(id)))
	}
	p.SendForm(form.NewMenu(detailsListMenu{buttons: buttons}, "Hoplite Codex Weapons").WithButtons(buttons...))
}

func sendLoreOnly(p *player.Player, weaponID string) {
	d, ok := Defs[weaponID]
	if !ok {
		return
	}
	body := ""
	for _, line := range d.Lore {
		body += line + "\n"
	}
	if body == "" {
		body = "§7No details found."
	}
	// Title is the weapon's own display name here, same as the original —
	// intentionally NOT one of the 4 skin-triggering strings, so this
	// screen keeps the plain default form look, matching the source.
	ok2 := form.NewButton("OK", "")
	p.SendForm(form.NewMenu(okOnly{ok: ok2}, d.DisplayName).WithBody(body).WithButtons(ok2))
}

// ---------------------------------------------------------------------
// Admin: reset all, with confirmation.
// ---------------------------------------------------------------------

type resetConfirmMenu struct {
	yes    form.Button
	cancel form.Button
}

func (m resetConfirmMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if pressed == m.yes {
		if !state.Ops.IsOp(p.XUID()) {
			return // re-check at submit time, not just at menu-open time
		}
		Mgr.ResetAll()
		p.Message("§aAll legendary weapons have been reset.")
	}
}

func sendResetConfirm(p *player.Player) {
	if !state.Ops.IsOp(p.XUID()) {
		return
	}
	yes := form.NewButton("Yes, reset everything", "")
	cancel := form.NewButton("Cancel", "")
	m := resetConfirmMenu{yes: yes, cancel: cancel}
	// Plain title, intentionally not one of the 4 skin strings — matches
	// the original, which never styled this particular screen either.
	p.SendForm(form.NewMenu(m, "Reset All Legendaries").
		WithBody("§cThis unlocks every legendary weapon for crafting again. Are you sure?").
		WithButtons(yes, cancel))
}
