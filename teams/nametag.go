package teams

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// RefreshNametag sets p's name tag to "§c[TeamName] §rPlayerName" if p is on
// a team, or back to their plain name otherwise. Call this after any change
// that could affect a player's team membership: join, create, accept
// invite, kick, leave, disband.
func RefreshNametag(m *Manager, p *player.Player) {
	t := m.TeamOfPlayer(p.Name())
	if t == nil {
		p.SetNameTag(p.Name())
		return
	}
	p.SetNameTag(fmt.Sprintf("%s[%s] §r%s", t.Color, t.Name, p.Name()))
}

// NotifyTeammates sends message to every online member of t except about,
// prefixed as a team notice ("§6[Team] §r..."). tx must be the transaction
// the caller is currently executing in — the tx passed to a cmd.Runnable's
// Run or a form.Submit, or (from a player.Handler callback) that handler's
// own player's live transaction. Deliberately does NOT use the
// state.FindOnline (non-tx) cache to look up teammates, since messaging a
// *player.Player fetched from outside its owning transaction is unsafe —
// see state.go's own warning on FindOnlineTx.
func NotifyTeammates(tx *world.Tx, t *Team, about *player.Player, message string) {
	for _, member := range t.Members {
		if member == about.Name() {
			continue
		}
		if p, ok := state.FindOnlineTx(tx, member); ok {
			p.Message("§6[Team] §r" + message)
		}
	}
}

// SendTeamChatMessage sends one chat-formatted message to every online
// member of t, including sender. See NotifyTeammates for the tx
// requirement.
func SendTeamChatMessage(tx *world.Tx, t *Team, sender *player.Player, message string) {
	formatted := fmt.Sprintf("§6[Team] §r%s%s§r: §f%s", t.Color, sender.Name(), message)
	for _, member := range t.Members {
		if p, ok := state.FindOnlineTx(tx, member); ok {
			p.Message(formatted)
		}
	}
}
