package rankforms

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/ranks"
	"velaris-dragonfly/state"
)

// ---------------------------------------------------------------------
// /rank — form-based rank management
// ---------------------------------------------------------------------
//
// /rank opens a root menu with three choices: Set Rank, Rank Colors, and
// Remove Rank. Each form's Submit runs inside the tx belonging to the
// admin who submitted it (see dragonfly's form docs), so every lookup here
// uses tx-bound srv.Players(tx) rather than the cached pointers in
// onlinePlayers — same safety concern as findOnlineTx above, just reached
// through state.Server instead of a passed-in srv.

// NOTE: form.Button's label is accessed via the exported Text string field
// (not a method) — confirmed after the initial build tried Text() and
// failed with "string is not a function".

// rankColorPalette is the fixed set of colors offered when recoloring a
// rank's tag or chat prefix.
var rankColorPalette = []struct{ Label, Code string }{
	{"Black", "§0"}, {"Dark Blue", "§1"}, {"Dark Green", "§2"}, {"Dark Aqua", "§3"},
	{"Dark Red", "§4"}, {"Dark Purple", "§5"}, {"Gold", "§6"}, {"Gray", "§7"},
	{"Dark Gray", "§8"}, {"Blue", "§9"}, {"Green", "§a"}, {"Aqua", "§b"},
	{"Red", "§c"}, {"Light Purple", "§d"}, {"Yellow", "§e"}, {"White", "§f"},
}

// findOnlinePlayerTx is a small wrapper around state.Server.Players(tx) for
// form Submit handlers, which already run inside a valid tx.
func findOnlinePlayerTx(tx *world.Tx, name string) (*player.Player, bool) {
	for p := range state.Server.Players(tx) {
		if p.Name() == name {
			return p, true
		}
	}
	return nil, false
}

// refreshTagsForRank updates the floating name tag of every online player
// currently holding rankName, e.g. after that rank's TagColor changes.
func refreshTagsForRank(tx *world.Tx, rankName string) {
	for p := range state.Server.Players(tx) {
		if state.Ranks.Of(p.XUID()) == rankName {
			ranks.ApplyNameTag(p, state.Ranks, state.RankDefs)
		}
	}
}

// --- Root menu: Set Rank / Rank Colors / Remove Rank ---

type rankRootForm struct{}

func (rankRootForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed.Text {
	case "Set Rank":
		sendRankTargetPicker(p, tx, "set")
	case "Rank Colors":
		sendRankColorTypeMenu(p, tx)
	case "Remove Rank":
		sendRankTargetPicker(p, tx, "remove")
	}
}

func sendRankRootMenu(p *player.Player) {
	menu := form.NewMenu(rankRootForm{}, "Rank Management").
		WithBody("Choose what you'd like to do.").
		WithButtons(
			form.NewButton("Set Rank", ""),
			form.NewButton("Rank Colors", ""),
			form.NewButton("Remove Rank", ""),
		)
	p.SendForm(menu)
}

// --- Player picker, shared by Set Rank and Remove Rank ---

type rankTargetForm struct{ mode string } // "set" or "remove"

func (f rankTargetForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	target, ok := findOnlinePlayerTx(tx, pressed.Text)
	if !ok {
		p.Message("§cThat player is no longer online.")
		return
	}
	if f.mode == "remove" {
		if err := state.Ranks.SetRank(target.XUID(), ranks.DefaultRankName); err != nil {
			p.Message("§cFailed to save rank: " + err.Error())
			return
		}
		ranks.ApplyNameTag(target, state.Ranks, state.RankDefs)
		target.Message("§7Your rank has been reset to Default.")
		p.Message(fmt.Sprintf("§aReset %s's rank to Default.", target.Name()))
		return
	}
	sendRankPicker(p, tx, target.Name())
}

func sendRankTargetPicker(p *player.Player, tx *world.Tx, mode string) {
	var buttons []form.Button
	for other := range state.Server.Players(tx) {
		buttons = append(buttons, form.NewButton(other.Name(), ""))
	}
	if len(buttons) == 0 {
		p.Message("§cNo players are online.")
		return
	}
	title := "Set Rank — pick a player"
	if mode == "remove" {
		title = "Remove Rank — pick a player"
	}
	menu := form.NewMenu(rankTargetForm{mode: mode}, title).
		WithBody("Select a player.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Rank picker (after choosing a target for "Set Rank") ---

type rankAssignForm struct{ targetName string }

func (f rankAssignForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	rankName := pressed.Text
	if _, ok := state.RankDefs.Get(rankName); !ok {
		p.Message("§cUnknown rank.")
		return
	}
	target, ok := findOnlinePlayerTx(tx, f.targetName)
	if !ok {
		p.Message("§cThat player is no longer online.")
		return
	}
	if err := state.Ranks.SetRank(target.XUID(), rankName); err != nil {
		p.Message("§cFailed to save rank: " + err.Error())
		return
	}
	ranks.ApplyNameTag(target, state.Ranks, state.RankDefs)
	target.Message("§aYour rank has been set to " + rankName + ".")
	p.Message(fmt.Sprintf("§aSet %s's rank to %s.", target.Name(), rankName))
}

func sendRankPicker(p *player.Player, tx *world.Tx, targetName string) {
	var buttons []form.Button
	for _, name := range state.RankDefs.Names() {
		buttons = append(buttons, form.NewButton(name, ""))
	}
	menu := form.NewMenu(rankAssignForm{targetName: targetName}, fmt.Sprintf("Set Rank — %s", targetName)).
		WithBody("Choose a rank to assign.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Color type menu: Tag Color vs Chat Color ---

type rankColorTypeForm struct{}

func (rankColorTypeForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed.Text {
	case "Tag Color (above head)":
		sendRankColorRankPicker(p, tx, "tag")
	case "Chat Color (in chat)":
		sendRankColorRankPicker(p, tx, "chat")
	case "Message Color (chat text)":
		sendRankColorRankPicker(p, tx, "message")
	}
}

func sendRankColorTypeMenu(p *player.Player, tx *world.Tx) {
	menu := form.NewMenu(rankColorTypeForm{}, "Rank Colors").
		WithBody("Tag Color is the floating name tag above a player. Chat Color is the \"[Rank] Name\" shown when they type. Message Color is the color of the message text itself.").
		WithButtons(
			form.NewButton("Tag Color (above head)", ""),
			form.NewButton("Chat Color (in chat)", ""),
			form.NewButton("Message Color (chat text)", ""),
		)
	p.SendForm(menu)
}

// --- Rank picker for recoloring ---

type rankColorRankForm struct{ kind string } // "tag" or "chat"

func (f rankColorRankForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if _, ok := state.RankDefs.Get(pressed.Text); !ok {
		p.Message("§cUnknown rank.")
		return
	}
	sendRankColorSwatchPicker(p, tx, f.kind, pressed.Text)
}

func sendRankColorRankPicker(p *player.Player, tx *world.Tx, kind string) {
	var buttons []form.Button
	for _, name := range state.RankDefs.Names() {
		buttons = append(buttons, form.NewButton(name, ""))
	}
	menu := form.NewMenu(rankColorRankForm{kind: kind}, "Pick a rank to recolor").
		WithBody("Choose which rank's color to change.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Color swatch picker (final step) ---

type rankColorSwatchForm struct {
	kind     string // "tag" or "chat"
	rankName string
}

func (f rankColorSwatchForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	var code string
	for _, c := range rankColorPalette {
		if c.Label == pressed.Text {
			code = c.Code
			break
		}
	}
	if code == "" {
		p.Message("§cUnknown color.")
		return
	}

	var err error
	if f.kind == "tag" {
		err = state.RankDefs.SetTagColor(f.rankName, code)
	} else {
		err = state.RankDefs.SetChatColor(f.rankName, code)
	}
	if err != nil {
		p.Message("§cFailed to save color: " + err.Error())
		return
	}
	if f.kind == "tag" {
		refreshTagsForRank(tx, f.rankName)
	}
	p.Message(fmt.Sprintf("§aUpdated %s's %s color.", f.rankName, f.kind))
}

func sendRankColorSwatchPicker(p *player.Player, tx *world.Tx, kind, rankName string) {
	var buttons []form.Button
	for _, c := range rankColorPalette {
		buttons = append(buttons, form.NewButton(c.Label, ""))
	}
	menu := form.NewMenu(rankColorSwatchForm{kind: kind, rankName: rankName}, fmt.Sprintf("Pick a color for %s", rankName)).
		WithBody("Choose a color.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// RankMenu is /rank — opens the rank management menu. In-game only, op only.
type RankMenu struct{}

func (RankMenu) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (RankMenu) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The /rank menu can only be opened in-game.")
		return
	}
	sendRankRootMenu(p)
}

