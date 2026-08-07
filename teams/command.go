package teams

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Mgr is the package-level team manager, set up by Init.
var Mgr *Manager

// Init loads (or creates) the teams file at path.
func Init(path string) error {
	m, err := NewManager(path)
	if err != nil {
		return err
	}
	Mgr = m
	return nil
}

// ---------------------------------------------------------------------
// Team chat mode — ephemeral (session-only) per-player toggle, matching
// TeamListener's static $teamChatEnabled array. Keyed by XUID rather than
// name (more stable identity, matches the rest of this repo's convention).
// ---------------------------------------------------------------------

var (
	chatMu      sync.Mutex
	chatEnabled = map[string]bool{}
)

// TeamChatEnabled reports whether xuid currently has team chat mode on.
func TeamChatEnabled(xuid string) bool {
	chatMu.Lock()
	defer chatMu.Unlock()
	return chatEnabled[xuid]
}

// SetTeamChatEnabled sets xuid's team chat mode.
func SetTeamChatEnabled(xuid string, enabled bool) {
	chatMu.Lock()
	defer chatMu.Unlock()
	if enabled {
		chatEnabled[xuid] = true
	} else {
		delete(chatEnabled, xuid)
	}
}

// ClearTeamChat removes xuid's team chat state — call this on quit so it
// doesn't linger past the session, matching the original's onQuit cleanup.
func ClearTeamChat(xuid string) {
	SetTeamChatEnabled(xuid, false)
}

// ---------------------------------------------------------------------
// /team — TeamMenu opens the form-based menu (see form.go); TeamChat
// handles the "chat" subcommand, both as a bare toggle and as a one-off
// "/team chat <message>". Register both under the same command name:
//
//	cmd.Register(cmd.New("team", "Team management menu.", nil, teams.TeamMenu{}, teams.TeamChat{}))
// ---------------------------------------------------------------------

// TeamMenu is plain "/team" with no arguments — opens the main menu form.
type TeamMenu struct{}

func (TeamMenu) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The team menu can only be opened in-game.")
		return
	}
	if Mgr == nil {
		output.Print("Teams aren't loaded yet.")
		return
	}
	SendMainMenu(p)
}

// TeamChat is "/team chat" (toggles team chat mode) or
// "/team chat <message...>" (sends one message to the team without
// toggling anything).
type TeamChat struct {
	Chat    cmd.SubCommand           `cmd:"chat"`
	Message cmd.Optional[cmd.Varargs] `cmd:"message"`
}

func (t TeamChat) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("Team chat can only be used in-game.")
		return
	}
	if Mgr == nil {
		output.Print("Teams aren't loaded yet.")
		return
	}
	team := Mgr.TeamOfPlayer(p.Name())
	if team == nil {
		output.Print("§cYou're not in a team.")
		return
	}

	msg, hasMsg := t.Message.Load()
	text := strings.TrimSpace(string(msg))
	if !hasMsg || text == "" {
		enabled := !TeamChatEnabled(p.XUID())
		SetTeamChatEnabled(p.XUID(), enabled)
		if enabled {
			output.Print("§aTeam chat enabled - everything you type now goes only to your team. Type §e/team chat§a again to turn it off.")
		} else {
			output.Print("§eTeam chat disabled - back to normal chat.")
		}
		return
	}

	SendTeamChatMessage(tx, team, p, text)
}
