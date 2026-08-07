package teams

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

var colorNames = []string{"Red", "Blue", "Green", "Yellow", "Aqua", "Light Purple", "Gold", "White"}
var colorCodes = map[string]string{
	"Red": "§c", "Blue": "§9", "Green": "§a", "Yellow": "§e",
	"Aqua": "§b", "Light Purple": "§d", "Gold": "§6", "White": "§f",
}

// ---------------------------------------------------------------------
// Main menu
// ---------------------------------------------------------------------

type mainMenu struct {
	buttons []form.Button
	actions []string
}

func (m mainMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b != pressed {
			continue
		}
		switch m.actions[i] {
		case "create":
			sendCreateForm(p)
		case "invite":
			sendInviteForm(p, tx)
		case "kick":
			sendKickForm(p)
		case "info":
			sendInfo(p)
		case "chat_toggle":
			enabled := !TeamChatEnabled(p.XUID())
			SetTeamChatEnabled(p.XUID(), enabled)
			if enabled {
				p.Message("§aTeam chat enabled - everything you type now goes only to your team. Type §e/team chat§a again to turn it off.")
			} else {
				p.Message("§eTeam chat disabled - back to normal chat.")
			}
		case "friendlyfire":
			toggleFriendlyFire(p)
		case "invites":
			sendInvitesForm(p, tx)
		case "leave":
			leaveTeam(p)
		case "disband":
			sendDisbandConfirm(p)
		}
		return
	}
}

// SendMainMenu opens the top-level /team form for p.
func SendMainMenu(p *player.Player) {
	t := Mgr.TeamOfPlayer(p.Name())
	isOwner := t != nil && Mgr.IsOwner(p.Name(), t.Name)

	var body string
	if t == nil {
		body = "§7You're not currently in a team.\n§8Create one, or check your pending invites below."
	} else {
		ff := "§aon"
		if !t.FriendlyFire {
			ff = "§coff"
		}
		owner := ""
		if isOwner {
			owner = " §e(you)"
		}
		body = fmt.Sprintf("%s§l%s§r §7(%d/%d members)\n§7Owner: §f%s%s\n§7Friendly fire: %s",
			t.Color, t.Name, len(t.Members), MaxMembers, t.Owner, owner, ff)
	}

	var buttons []form.Button
	var actions []string

	if t == nil {
		buttons = append(buttons, form.NewButton("§a✚ Create a team", ""))
		actions = append(actions, "create")
	}
	if isOwner {
		buttons = append(buttons, form.NewButton("§b✉ Invite to team", ""))
		actions = append(actions, "invite")
		if len(t.Members) > 1 {
			buttons = append(buttons, form.NewButton("§c✖ Kick from team", ""))
			actions = append(actions, "kick")
		}
	}
	if t != nil {
		buttons = append(buttons, form.NewButton("§eℹ Team info", ""))
		actions = append(actions, "info")
		chatLabel := "§6✎ Team chat: §cOFF"
		if TeamChatEnabled(p.XUID()) {
			chatLabel = "§6✎ Team chat: §aON"
		}
		buttons = append(buttons, form.NewButton(chatLabel, ""))
		actions = append(actions, "chat_toggle")
	}
	if isOwner {
		label := "§a⚔ Enable friendly fire"
		if t.FriendlyFire {
			label = "§c⚔ Disable friendly fire"
		}
		buttons = append(buttons, form.NewButton(label, ""))
		actions = append(actions, "friendlyfire")
	}
	buttons = append(buttons, form.NewButton("§d✉ Team invites", ""))
	actions = append(actions, "invites")
	if t != nil && !isOwner {
		buttons = append(buttons, form.NewButton("§7➜ Leave team", ""))
		actions = append(actions, "leave")
	}
	if isOwner {
		buttons = append(buttons, form.NewButton("§4☠ Disband team", ""))
		actions = append(actions, "disband")
	}

	m := mainMenu{buttons: buttons, actions: actions}
	p.SendForm(form.NewMenu(m, "§l§6Team Menu").WithBody(body).WithButtons(buttons...))
}

// ---------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------

type createForm struct {
	Name  form.Input
	Color form.Dropdown
}

func (f createForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	name := strings.TrimSpace(f.Name.Value())
	color := colorCodes[f.Color.Options[f.Color.Value()]]
	if color == "" {
		color = "§f"
	}
	if errMsg := Mgr.CreateTeam(p.Name(), name, color); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	p.Message(fmt.Sprintf("§aTeam \"%s\" created!", name))
	RefreshNametag(Mgr, p)
}

func sendCreateForm(p *player.Player) {
	f := createForm{
		Name:  form.Input{Text: "Team name", Placeholder: "e.g. Spartans"},
		Color: form.Dropdown{Text: "Nametag color", Options: colorNames},
	}
	p.SendForm(form.New(f, "Create a Team"))
}

// ---------------------------------------------------------------------
// Invite
// ---------------------------------------------------------------------

type inviteMenu struct {
	buttons []form.Button
	targets []string // "" for the first "invite by IGN" button
}

func (m inviteMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b != pressed {
			continue
		}
		if m.targets[i] == "" {
			sendInviteByIGNForm(p)
			return
		}
		sendInvite(p, m.targets[i], tx)
		return
	}
}

func sendInviteForm(p *player.Player, tx *world.Tx) {
	buttons := []form.Button{form.NewButton("§b✎ Invite by IGN", "")}
	targets := []string{""}
	for online := range state.Server.Players(tx) {
		if online.Name() == p.Name() {
			continue
		}
		buttons = append(buttons, form.NewButton("§f"+online.Name(), ""))
		targets = append(targets, online.Name())
	}
	m := inviteMenu{buttons: buttons, targets: targets}
	p.SendForm(form.NewMenu(m, "§l§bInvite to Team").
		WithBody("§7Pick an online player, or invite someone offline by IGN.").
		WithButtons(buttons...))
}

type inviteIGNForm struct {
	Name form.Input
}

func (f inviteIGNForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	target := strings.TrimSpace(f.Name.Value())
	if target != "" {
		sendInvite(p, target, tx)
	}
}

func sendInviteByIGNForm(p *player.Player) {
	f := inviteIGNForm{Name: form.Input{Text: "Player name", Placeholder: "Exact in-game name"}}
	p.SendForm(form.New(f, "§l§bInvite by IGN"))
}

// sendInvite records the invite and messages both sides. tx may be nil if
// the caller doesn't have one (e.g. invite-by-IGN, since the target may be
// offline) — NotifyTeammates-style messaging to the target is skipped in
// that case and picked up next time they check /team -> invites instead.
func sendInvite(p *player.Player, target string, tx *world.Tx) {
	if errMsg := Mgr.Invite(p.Name(), target); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	p.Message(fmt.Sprintf("§aInvited %s to your team.", target))
	if tx != nil {
		if targetPlayer, ok := state.FindOnlineTx(tx, target); ok {
			targetPlayer.Message(fmt.Sprintf("§e%s invited you to their team! Check /team -> Team invites.", p.Name()))
		}
	}
}

// ---------------------------------------------------------------------
// Kick
// ---------------------------------------------------------------------

type kickMenu struct {
	buttons []form.Button
	targets []string
}

func (m kickMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b == pressed {
			sendKickConfirm(p, m.targets[i])
			return
		}
	}
}

func sendKickForm(p *player.Player) {
	t := Mgr.TeamOfPlayer(p.Name())
	if t == nil || !Mgr.IsOwner(p.Name(), t.Name) {
		p.Message("§cYou don't own a team.")
		return
	}
	var buttons []form.Button
	var targets []string
	for _, member := range t.Members {
		if member == p.Name() {
			continue
		}
		kills, deaths := t.Kills[member], t.Deaths[member]
		buttons = append(buttons, form.NewButton(fmt.Sprintf("§f%s\n§7K/D: %d/%d", member, kills, deaths), ""))
		targets = append(targets, member)
	}
	if len(targets) == 0 {
		p.Message("§eThere's no one else on your team to kick.")
		return
	}
	m := kickMenu{buttons: buttons, targets: targets}
	p.SendForm(form.NewMenu(m, "§l§cKick from Team").
		WithBody(fmt.Sprintf("§7Select a member to remove from %s%s§7.", t.Color, t.Name)).
		WithButtons(buttons...))
}

type kickConfirmMenu struct {
	target      string
	yes, cancel form.Button
}

func (m kickConfirmMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok || pressed != m.yes {
		return
	}
	if errMsg := Mgr.Kick(p.Name(), m.target); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	p.Message(fmt.Sprintf("§a%s was kicked from your team.", m.target))
	if kicked, ok := state.FindOnlineTx(tx, m.target); ok {
		RefreshNametag(Mgr, kicked)
		kicked.Message(fmt.Sprintf("§cYou were kicked from your team by %s.", p.Name()))
	}
}

func sendKickConfirm(p *player.Player, target string) {
	yes := form.NewButton("§c✖ Yes, kick them", "")
	cancel := form.NewButton("§a✔ Cancel", "")
	m := kickConfirmMenu{target: target, yes: yes, cancel: cancel}
	p.SendForm(form.NewMenu(m, "§l§cConfirm Kick").
		WithBody(fmt.Sprintf("§7Are you sure you want to kick §f%s§7 from your team?", target)).
		WithButtons(yes, cancel))
}

// ---------------------------------------------------------------------
// Disband
// ---------------------------------------------------------------------

type disbandMenu struct {
	members     []string
	yes, cancel form.Button
}

func (m disbandMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok || pressed != m.yes {
		return
	}
	if errMsg := Mgr.Disband(p.Name()); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	p.Message("§aTeam disbanded.")
	RefreshNametag(Mgr, p)
	for _, member := range m.members {
		if mp, ok := state.FindOnlineTx(tx, member); ok {
			RefreshNametag(Mgr, mp)
		}
	}
}

func sendDisbandConfirm(p *player.Player) {
	t := Mgr.TeamOfPlayer(p.Name())
	if t == nil {
		p.Message("§cYou don't own a team.")
		return
	}
	yes := form.NewButton("§4☠ Yes, disband it", "")
	cancel := form.NewButton("§a✔ Cancel", "")
	m := disbandMenu{members: append([]string(nil), t.Members...), yes: yes, cancel: cancel}
	p.SendForm(form.NewMenu(m, "§l§4Confirm Disband").
		WithBody(fmt.Sprintf("§7Are you sure you want to disband %s%s§7?\n§8This removes all %d members and cannot be undone.", t.Color, t.Name, len(t.Members))).
		WithButtons(yes, cancel))
}

// ---------------------------------------------------------------------
// Friendly fire / leave / info (no form needed beyond a message)
// ---------------------------------------------------------------------

func toggleFriendlyFire(p *player.Player) {
	t := Mgr.TeamOfPlayer(p.Name())
	if t == nil {
		return
	}
	newVal := !t.FriendlyFire
	if errMsg := Mgr.SetFriendlyFire(p.Name(), newVal); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	if newVal {
		p.Message("§aFriendly fire enabled.")
	} else {
		p.Message("§cFriendly fire disabled.")
	}
}

func leaveTeam(p *player.Player) {
	if errMsg := Mgr.Leave(p.Name()); errMsg != "" {
		p.Message("§c" + errMsg)
		return
	}
	p.Message("§aYou left your team.")
	RefreshNametag(Mgr, p)
}

func sendInfo(p *player.Player) {
	t := Mgr.TeamOfPlayer(p.Name())
	if t == nil {
		p.Message("§cYou're not in a team.")
		return
	}
	lines := []string{fmt.Sprintf("§6§l▎ Team: §r%s%s §7(%d/%d)", t.Color, t.Name, len(t.Members), MaxMembers)}
	lines = append(lines, "§7Owner: §f"+t.Owner)
	if t.FriendlyFire {
		lines = append(lines, "§7Friendly fire: §aon")
	} else {
		lines = append(lines, "§7Friendly fire: §coff")
	}
	lines = append(lines, "§6Members:")
	for _, member := range t.Members {
		tag := ""
		if member == t.Owner {
			tag = " §e(owner)"
		}
		lines = append(lines, fmt.Sprintf("§7 • §f%s%s §7| K/D: %d/%d", member, tag, t.Kills[member], t.Deaths[member]))
	}
	p.Message(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------
// Invites
// ---------------------------------------------------------------------

type invitesMenu struct {
	buttons []form.Button
	teams   []string
}

func (m invitesMenu) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	for i, b := range m.buttons {
		if b != pressed {
			continue
		}
		teamName := m.teams[i]
		if errMsg := Mgr.AcceptInvite(p.Name(), teamName); errMsg != "" {
			p.Message("§c" + errMsg)
			return
		}
		p.Message(fmt.Sprintf("§aYou joined \"%s\"!", teamName))
		RefreshNametag(Mgr, p)
		if t := Mgr.Team(teamName); t != nil {
			NotifyTeammates(tx, t, p, fmt.Sprintf("§a%s has joined the team.", p.Name()))
		}
		return
	}
}

func sendInvitesForm(p *player.Player, tx *world.Tx) {
	invites := Mgr.Invites(p.Name())
	if len(invites) == 0 {
		p.Message("§eYou have no pending team invites.")
		return
	}
	var buttons []form.Button
	for _, name := range invites {
		buttons = append(buttons, form.NewButton("§d✉ "+name, ""))
	}
	m := invitesMenu{buttons: buttons, teams: invites}
	p.SendForm(form.NewMenu(m, "§l§dTeam Invites").
		WithBody("§7Select a team below to accept its invite.").
		WithButtons(buttons...))
}
