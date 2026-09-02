package teams

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// /team and /team chat [message] — ported from TeamCommand::execute().
//
// The PHP original was a single command class that special-cased its
// first argument ("chat") in execute(). Dragonfly's cmd package instead
// wants one Go type per distinct argument shape, both registered under
// the same command name — see TimeSet/TimeQuery or pvp.On/Off/Block
// elsewhere in this project for the same pattern. So this is split into
// TeamMenu (bare /team, opens the form menu) and TeamChat (/team chat
// [message], handles both the toggle and the one-off-message forms of
// the PHP original's handleChatSubcommand).
//
// Register both together in main.go's registerCommands():
//
//	cmd.Register(cmd.New("team", "Team management menu.", nil, teams.TeamMenu{}, teams.TeamChat{}))
//
// No Allow() method on either — hoplite.team defaulted to "true" (every
// player) in the original plugin.yml, so /team is unrestricted here too,
// matching commands.Ping/Coords/Feed's lack of an Allow method elsewhere
// in this project.
// ---------------------------------------------------------------------

// TeamMenu is bare /team — opens the main team menu form. In-game only.
type TeamMenu struct{}

func (TeamMenu) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The /team menu can only be opened in-game.")
		return
	}
	OpenMainMenu(p, tx)
}

// TeamChat is /team chat [message]. With no message, this toggles "team
// chat mode" for the sender - while on, everything they type in normal
// chat is intercepted (see hooks.HandleChat, called from
// players.PlayerHandler.HandleChat) and routed to their team only. With a
// message, it sends that one message to the team without toggling
// anything, for a quick one-off without switching modes. Matches
// TeamCommand::handleChatSubcommand exactly.
//
// Message is typed cmd.Optional[string] rather than required so that bare
// "/team chat" (the toggle form) still parses; when present, dragonfly's
// trailing-string parameter behavior captures the rest of the line as one
// argument (see commands.Kick/Ban's Reason field elsewhere in this
// project for the same pattern), so no manual space-joining is needed.
type TeamChat struct {
	Chat    cmd.SubCommand       `cmd:"chat"`
	Message cmd.Optional[string] `cmd:"message"`
}

func (tc TeamChat) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("Team chat can only be used in-game.")
		return
	}
	if _, ok := Mgr.GetTeamOfPlayer(p.Name()); !ok {
		p.Message("§cYou're not in a team.")
		return
	}

	message, has := tc.Message.Load()
	if !has || message == "" {
		enabled := !IsTeamChatEnabled(p)
		SetTeamChatEnabled(p, enabled)
		if enabled {
			p.Message("§aTeam chat enabled - everything you type now goes only to your team. Type §e/team chat§a again to turn it off.")
		} else {
			p.Message("§eTeam chat disabled - back to normal chat.")
		}
		return
	}

	t, _ := Mgr.GetTeamOfPlayer(p.Name())
	SendTeamChatMessage(p, t, message)
}
