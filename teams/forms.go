package teams

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// ---------------------------------------------------------------------
// /team's form UI — ported from TeamCommand.php's openMainMenu and every
// form-opening method below it. Follows this project's existing form
// conventions (see rankforms/rankforms.go and knockback/form.go): each
// menu/form is its own Submittable type, and menus dispatch on
// form.Button.Text (rankforms.go's rankRootForm does the same). Player
// listings built while a tx is available (e.g. the online-player list in
// the invite/kick menus) go through state.Server.Players(tx), matching
// rankforms.go's findOnlinePlayerTx; cross-player messaging elsewhere
// (invite notifications, kick/disband notices) uses the non-tx
// state.FindOnline, matching commands.Op/Deop's precedent of calling
// .Message() directly on that cached pointer.
//
// CONFIRMED BY BUILD: form.Dropdown.Value() returns int — the selected
// option's index into the slice passed to NewDropdown — not a string like
// Input.Value()/Toggle.Value(). This was originally a guess (the rest of
// this project's one custom form never uses a Dropdown), flagged as
// unverified; a real `go build` against it came back with "cannot use
// f.Color.Value() (value of type int) as string value in argument to
// colorCode", which is what confirmed it. createTeamForm.Submit below
// converts that index back to a §-code via colorCodeByIndex
// (manager.go), indexing the same Colors slice ColorNames() was built
// from, so the two stay in sync by construction.
// ---------------------------------------------------------------------

// OpenMainMenu opens /team's root menu for p — the button list and
// summary header exactly mirror TeamCommand::openMainMenu /
// buildMenuHeader.
func OpenMainMenu(p *player.Player, tx *world.Tx) {
	team, inTeam := Mgr.GetTeamOfPlayer(p.Name())
	isOwner := inTeam && Mgr.IsOwner(p.Name(), team.Name)

	body := mainMenuHeader(team, inTeam, isOwner)

	var buttons []form.Button
	if !inTeam {
		buttons = append(buttons, form.NewButton("§a✚ Create a team", ""))
	}
	if isOwner {
		buttons = append(buttons, form.NewButton("§b✉ Invite to team", ""))
		if len(team.Members) > 1 {
			buttons = append(buttons, form.NewButton("§c✖ Kick from team", ""))
		}
	}
	if inTeam {
		buttons = append(buttons, form.NewButton("§eℹ Team info", ""))
		chatLabel := "§6✎ Team chat: §c OFF"
		if IsTeamChatEnabled(p) {
			chatLabel = "§6✎ Team chat: §a ON"
		}
		buttons = append(buttons, form.NewButton(chatLabel, ""))
	}
	if isOwner {
		ffLabel := "§a⚔ Enable friendly fire"
		if team.FriendlyFire {
			ffLabel = "§c⚔ Disable friendly fire"
		}
		buttons = append(buttons, form.NewButton(ffLabel, ""))
	}
	buttons = append(buttons, form.NewButton("§d✉ Team invites", ""))
	if inTeam && !isOwner {
		buttons = append(buttons, form.NewButton("§7➜ Leave team", ""))
	}
	if isOwner {
		buttons = append(buttons, form.NewButton("§4☠ Disband team", ""))
	}

	p.SendForm(form.NewMenu(mainMenuForm{}, "§l§6Team Menu").WithBody(body).WithButtons(buttons...))
}

func mainMenuHeader(team Team, inTeam, isOwner bool) string {
	if !inTeam {
		return "§7You're not currently in a team.\n§8Create one, or check your pending invites below."
	}
	owner := "§7Owner: §f" + team.Owner
	if isOwner {
		owner += " §e(you)"
	}
	ff := "§7Friendly fire: §coff"
	if team.FriendlyFire {
		ff = "§7Friendly fire: §aon"
	}
	return fmt.Sprintf("%s§l%s§r§7  (%d/%d members)\n%s\n%s",
		team.Color, team.Name, len(team.Members), MaxMembers, owner, ff)
}

// mainMenuForm dispatches OpenMainMenu's button presses. Matched on
// button text with a HasSuffix check for the two state-dependent buttons
// (team chat / friendly fire), since their label includes the current
// on/off state.
type mainMenuForm struct{}

func (mainMenuForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch {
	case pressed.Text == "§a✚ Create a team":
		openCreateForm(p)
	case pressed.Text == "§b✉ Invite to team":
		openInviteForm(p, tx)
	case pressed.Text == "§c✖ Kick from team":
		openKickForm(p, tx)
	case pressed.Text == "§eℹ Team info":
		showInfo(p)
	case strings.HasPrefix(pressed.Text, "§6✎ Team chat:"):
		toggleTeamChat(p)
	case strings.HasPrefix(pressed.Text, "§a⚔ Enable friendly fire"), strings.HasPrefix(pressed.Text, "§c⚔ Disable friendly fire"):
		toggleFriendlyFire(p)
	case pressed.Text == "§d✉ Team invites":
		openInvitesForm(p)
	case pressed.Text == "§7➜ Leave team":
		leaveTeam(p, tx)
	case pressed.Text == "§4☠ Disband team":
		confirmDisband(p, tx)
	}
}

// --- Create a team ---

type createTeamForm struct {
	Name  form.Input
	Color form.Dropdown
}

func (f createTeamForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	name := strings.TrimSpace(f.Name.Value())
	color := colorCodeByIndex(f.Color.Value())
	if msg := Mgr.CreateTeam(p.Name(), name, color); msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§aTeam \"" + name + "\" created!")
	RefreshNameTag(p)
}

func openCreateForm(p *player.Player) {
	f := createTeamForm{
		Name:  form.NewInput("Team name", "e.g. Spartans", ""),
		Color: form.NewDropdown("Nametag color", ColorNames(), 0),
	}
	p.SendForm(form.New(f, "Create a Team"))
}

// --- Invite to team ---

type inviteMenuForm struct{}

func (f inviteMenuForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if pressed.Text == "§b✎ Invite by IGN" {
		openInviteByIGNForm(p)
		return
	}
	sendInvite(p, strings.TrimPrefix(pressed.Text, "§f"))
}

func openInviteForm(p *player.Player, tx *world.Tx) {
	buttons := []form.Button{form.NewButton("§b✎ Invite by IGN", "")}
	for other := range state.Server.Players(tx) {
		if strings.EqualFold(other.Name(), p.Name()) {
			continue
		}
		buttons = append(buttons, form.NewButton("§f"+other.Name(), ""))
	}
	menu := form.NewMenu(inviteMenuForm{}, "§l§bInvite to Team").
		WithBody("§7Pick an online player, or invite someone offline by IGN.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

type inviteByIGNForm struct {
	Name form.Input
}

func (f inviteByIGNForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	target := strings.TrimSpace(f.Name.Value())
	if target != "" {
		sendInvite(p, target)
	}
}

func openInviteByIGNForm(p *player.Player) {
	f := inviteByIGNForm{Name: form.NewInput("Player name", "Exact in-game name", "")}
	p.SendForm(form.New(f, "§l§bInvite by IGN"))
}

func sendInvite(p *player.Player, target string) {
	if msg := Mgr.Invite(p.Name(), target); msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§aInvited " + target + " to your team.")
	if targetPlayer, ok := state.FindOnline(target); ok {
		targetPlayer.Message("§e" + p.Name() + " invited you to their team! Check /team -> Team invites.")
	}
}

// --- Kick from team ---

type kickMenuForm struct{}

func (f kickMenuForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	name := strings.TrimPrefix(pressed.Text, "§f")
	// pressed.Text includes the K/D line too (see openKickForm) — the
	// button text before the newline is the exact member name.
	if i := strings.IndexByte(name, '\n'); i >= 0 {
		name = name[:i]
	}
	confirmKick(p, tx, name)
}

func openKickForm(p *player.Player, tx *world.Tx) {
	team, ok := Mgr.GetTeamOfPlayer(p.Name())
	if !ok || !Mgr.IsOwner(p.Name(), team.Name) {
		p.Message("§cYou don't own a team.")
		return
	}
	var buttons []form.Button
	for _, member := range team.Members {
		if strings.EqualFold(member, p.Name()) {
			continue
		}
		kills, deaths := team.Kills[member], team.Deaths[member]
		buttons = append(buttons, form.NewButton(fmt.Sprintf("§f%s\n§7K/D: %d/%d", member, kills, deaths), ""))
	}
	if len(buttons) == 0 {
		p.Message("§eThere's no one else on your team to kick.")
		return
	}
	menu := form.NewMenu(kickMenuForm{}, "§l§cKick from Team").
		WithBody("§7Select a member to remove from §r" + team.Color + team.Name + "§7.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

type confirmKickForm struct{ target string }

func (f confirmKickForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok || pressed.Text != "§c✖ Yes, kick them" {
		return
	}
	msg := Mgr.Kick(p.Name(), f.target)
	if msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§a" + f.target + " was kicked from your team.")
	if kicked, ok := state.FindOnline(f.target); ok {
		RefreshNameTag(kicked)
		kicked.Message("§cYou were kicked from your team by " + p.Name() + ".")
	}
}

func confirmKick(p *player.Player, tx *world.Tx, target string) {
	menu := form.NewMenu(confirmKickForm{target: target}, "§l§cConfirm Kick").
		WithBody("§7Are you sure you want to kick §f" + target + "§7 from your team?").
		WithButtons(
			form.NewButton("§c✖ Yes, kick them", ""),
			form.NewButton("§a✔ Cancel", ""),
		)
	p.SendForm(menu)
}

// --- Disband ---

type confirmDisbandForm struct{ team Team }

func (f confirmDisbandForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok || pressed.Text != "§4☠ Yes, disband it" {
		return
	}
	msg := Mgr.Disband(p.Name())
	if msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§aTeam disbanded.")
	RefreshNameTag(p)
	for _, memberName := range f.team.Members {
		if member, ok := state.FindOnline(memberName); ok {
			RefreshNameTag(member)
		}
	}
}

func confirmDisband(p *player.Player, tx *world.Tx) {
	team, ok := Mgr.GetTeamOfPlayer(p.Name())
	if !ok {
		p.Message("§cYou don't own a team.")
		return
	}
	menu := form.NewMenu(confirmDisbandForm{team: team}, "§l§4Confirm Disband").
		WithBody(fmt.Sprintf("§7Are you sure you want to disband §r%s%s§7?\n§8This removes all %d members and cannot be undone.",
			team.Color, team.Name, len(team.Members))).
		WithButtons(
			form.NewButton("§4☠ Yes, disband it", ""),
			form.NewButton("§a✔ Cancel", ""),
		)
	p.SendForm(menu)
}

// --- Friendly fire toggle ---

func toggleFriendlyFire(p *player.Player) {
	team, ok := Mgr.GetTeamOfPlayer(p.Name())
	if !ok {
		return
	}
	newState := !team.FriendlyFire
	if msg := Mgr.SetFriendlyFire(p.Name(), newState); msg != "" {
		p.Message("§c" + msg)
		return
	}
	if newState {
		p.Message("§aFriendly fire enabled.")
	} else {
		p.Message("§cFriendly fire disabled.")
	}
}

// --- Team chat toggle (from the menu button — /team chat does the same
// thing directly, see command.go) ---

func toggleTeamChat(p *player.Player) {
	enabled := !IsTeamChatEnabled(p)
	SetTeamChatEnabled(p, enabled)
	if enabled {
		p.Message("§aTeam chat enabled - everything you type now goes only to your team. Type §e/team chat§a again to turn it off.")
	} else {
		p.Message("§eTeam chat disabled - back to normal chat.")
	}
}

// --- Team info ---

func showInfo(p *player.Player) {
	team, ok := Mgr.GetTeamOfPlayer(p.Name())
	if !ok {
		p.Message("§cYou're not in a team.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "§6§l▎ Team: §r%s%s§7  (%d/%d)\n", team.Color, team.Name, len(team.Members), MaxMembers)
	fmt.Fprintf(&b, "§7Owner: §f%s\n", team.Owner)
	ff := "§coff"
	if team.FriendlyFire {
		ff = "§aon"
	}
	fmt.Fprintf(&b, "§7Friendly fire: %s\n", ff)
	b.WriteString("§6Members:\n")
	for _, member := range team.Members {
		tag := ""
		if strings.EqualFold(member, team.Owner) {
			tag = " §e(owner)"
		}
		fmt.Fprintf(&b, "§7 • §f%s%s§7 | K/D: %d/%d\n", member, tag, team.Kills[member], team.Deaths[member])
	}
	p.Message(strings.TrimRight(b.String(), "\n"))
}

// --- Team invites ---

type invitesMenuForm struct{}

func (f invitesMenuForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	teamName := strings.TrimPrefix(pressed.Text, "§d✉ ")
	msg := Mgr.AcceptInvite(p.Name(), teamName)
	if msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§aYou joined \"" + teamName + "\"!")
	RefreshNameTag(p)
	if team, ok := Mgr.GetTeam(teamName); ok {
		NotifyTeammates(team, p, "§a"+p.Name()+" has joined the team.")
	}
}

func openInvitesForm(p *player.Player) {
	invites := Mgr.GetInvites(p.Name())
	if len(invites) == 0 {
		p.Message("§eYou have no pending team invites.")
		return
	}
	var buttons []form.Button
	for _, name := range invites {
		buttons = append(buttons, form.NewButton("§d✉ "+name, ""))
	}
	menu := form.NewMenu(invitesMenuForm{}, "§l§dTeam Invites").
		WithBody("§7Select a team below to accept its invite.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Leave team ---

func leaveTeam(p *player.Player, tx *world.Tx) {
	msg := Mgr.Leave(p.Name())
	if msg != "" {
		p.Message("§c" + msg)
		return
	}
	p.Message("§aYou left your team.")
	RefreshNameTag(p)
}
