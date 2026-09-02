package teams

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/player"

	"velaris-dragonfly/ranks"
	"velaris-dragonfly/state"
)

// ---------------------------------------------------------------------
// Event hooks — a port of TeamListener.php. Dragonfly has no separate
// "Listener" registration step like PMMP; instead these are plain
// functions that main.go and players.PlayerHandler call directly from
// their own join/quit/chat/damage/death handling. See the bottom of this
// file for exactly which call goes where.
// ---------------------------------------------------------------------

// chatMu/chatEnabled track, per lowercase player name, whether team chat
// mode is currently on for them (toggled by /team chat with no message —
// see command.go). Session-only, like the PHP original's static array:
// cleared on quit by ClearChatState.
var (
	chatMu      sync.Mutex
	chatEnabled = map[string]bool{}
)

// IsTeamChatEnabled reports whether p currently has team chat mode on.
func IsTeamChatEnabled(p *player.Player) bool {
	chatMu.Lock()
	defer chatMu.Unlock()
	return chatEnabled[lower(p.Name())]
}

// SetTeamChatEnabled turns team chat mode on/off for p.
func SetTeamChatEnabled(p *player.Player, enabled bool) {
	chatMu.Lock()
	defer chatMu.Unlock()
	chatEnabled[lower(p.Name())] = enabled
}

// ClearChatState drops p's team chat mode flag. Call this on quit so it
// doesn't linger into their next session under a shared struct pointer
// (mirrors the PHP original's unset() in TeamListener::onQuit).
func ClearChatState(p *player.Player) {
	chatMu.Lock()
	defer chatMu.Unlock()
	delete(chatEnabled, lower(p.Name()))
}

// SendTeamChatMessage sends one chat message to every online member of t,
// including the sender, formatted as team chat. Uses state.FindOnline
// (not a tx-bound lookup) — the same non-tx cross-player .Message() calls
// already used by commands.Op/commands.Deop in this project, since
// Message() is a session-communication call rather than a world mutation.
func SendTeamChatMessage(sender *player.Player, t Team, message string) {
	formatted := "§6[Team] §r" + t.Color + sender.Name() + "§r: §f" + message
	for _, name := range t.Members {
		if member, ok := state.FindOnline(name); ok {
			member.Message(formatted)
		}
	}
}

// NotifyTeammates sends message to every online member of t except about.
func NotifyTeammates(t Team, about *player.Player, message string) {
	for _, name := range t.Members {
		if strings.EqualFold(name, about.Name()) {
			continue
		}
		if member, ok := state.FindOnline(name); ok {
			member.Message("§6[Team] §r" + message)
		}
	}
}

// HandleChat is called from players.PlayerHandler.HandleChat before any
// normal chat formatting/broadcast happens. If p has team chat mode on
// and is still in a team, this routes message to their team and returns
// true — the caller must not also broadcast the message normally. If team
// chat mode was on but p is no longer in a team (left/got disbanded on),
// this turns it back off, tells them why, and returns false so the
// message falls through to normal chat instead of silently vanishing —
// matches TeamListener::onChat exactly.
func HandleChat(p *player.Player, message string) bool {
	if !IsTeamChatEnabled(p) {
		return false
	}
	t, ok := Mgr.GetTeamOfPlayer(p.Name())
	if !ok {
		SetTeamChatEnabled(p, false)
		p.Message("§eTeam chat turned off - you're no longer in a team.")
		return false
	}
	SendTeamChatMessage(p, t, message)
	return true
}

// FriendlyFireBlocked reports whether damage from damager to victim
// should be cancelled because they're on the same team with friendly fire
// off, and if so, the message to show damager. Ported from
// TeamListener::onDamage. Call this from
// PlayerHandler.HandleAttackEntity before applying knockback.
func FriendlyFireBlocked(victim, damager *player.Player) (bool, string) {
	victimTeam, ok1 := Mgr.GetTeamOfPlayer(victim.Name())
	damagerTeam, ok2 := Mgr.GetTeamOfPlayer(damager.Name())
	if !ok1 || !ok2 || victimTeam.Name != damagerTeam.Name {
		return false, ""
	}
	if !victimTeam.FriendlyFire {
		return true, "§cFriendly fire is disabled for your team."
	}
	return false, ""
}

// OnDeath records a death for victim and, if killerName is non-empty, a
// kill for the killer. Call from PlayerHandler.HandleDeath. Ported from
// TeamListener::onDeath (which read EntityDeathEvent's last damage cause
// itself; here the caller already knows the attacker from its own death
// handling, so it's passed straight in).
func OnDeath(victimName, killerName string) {
	if Mgr == nil {
		return
	}
	Mgr.RecordDeath(victimName)
	if killerName != "" {
		Mgr.RecordKill(killerName)
	}
}

// HandleJoin notifies victim's teammates that they're back and refreshes
// their nametag. Call once per join, from main.go's srv.Accept() loop —
// ported from TeamListener::onJoin (minus the join-message suppression,
// which main.go already doesn't do by default).
func HandleJoin(p *player.Player) {
	RefreshNameTag(p)
	if Mgr == nil {
		return
	}
	if t, ok := Mgr.GetTeamOfPlayer(p.Name()); ok {
		NotifyTeammates(t, p, "§a"+p.Name()+" has joined back.")
	}
}

// HandleQuit notifies quitting's teammates that they've left, and clears
// their session-only team chat flag. Call once per quit, from
// players.PlayerHandler.HandleQuit — ported from TeamListener::onQuit.
func HandleQuit(p *player.Player) {
	if Mgr != nil {
		if t, ok := Mgr.GetTeamOfPlayer(p.Name()); ok {
			NotifyTeammates(t, p, "§c"+p.Name()+" has left the game.")
		}
	}
	ClearChatState(p)
}

// ---------------------------------------------------------------------
// Nametag
// ---------------------------------------------------------------------
//
// INTEGRATION NOTE: in the original PMMP setup, HopliteLegendary's teams
// (TeamListener::refreshNametag) and the separate "Ranks" plugin
// (RankModule) each unconditionally overwrote the player's whole nametag,
// so at most one of the two prefixes could actually be showing at once,
// whichever plugin last touched it. Since this Dragonfly server has both
// ported into the same codebase, RefreshNameTag below combines them —
// rank prefix, then team prefix, then the player's name — rather than
// silently dropping one. If you'd rather match the PHP original's
// last-write-wins behavior (team-only, no rank prefix, or vice versa),
// say so and this is a one-line change.
//
// This does NOT modify ranks.ApplyNameTag (kept untouched to avoid any
// import-cycle risk — ranks would have to import teams for that function
// to include a team prefix, and nothing here requires it). Call
// teams.RefreshNameTag anywhere you'd otherwise call ranks.ApplyNameTag
// and want the team prefix included too: on join (already done via
// HandleJoin above) and after create/disband/kick/leave/invite-accept
// (already done in forms.go).

// NameTagPrefix returns the "§color[TeamName] §r" prefix to prepend to a
// player's nametag, or "" if they're not in a team.
func NameTagPrefix(playerName string) string {
	if Mgr == nil {
		return ""
	}
	t, ok := Mgr.GetTeamOfPlayer(playerName)
	if !ok {
		return ""
	}
	return t.Color + "[" + t.Name + "] §r"
}

// RefreshNameTag rebuilds p's full nametag: rank prefix (if ranks/rank
// defs are loaded) + team prefix (if p is in a team) + their name. See
// the INTEGRATION NOTE above for why this combines both rather than
// matching either plugin's original standalone behavior.
func RefreshNameTag(p *player.Player) {
	rankPrefix := ""
	if state.Ranks != nil && state.RankDefs != nil {
		rankName := state.Ranks.Of(p.XUID())
		def, ok := state.RankDefs.Get(rankName)
		if !ok {
			def, _ = state.RankDefs.Get(ranks.DefaultRankName)
		}
		rankPrefix = def.NameTagPrefix()
	}
	p.SetNameTag(rankPrefix + NameTagPrefix(p.Name()) + p.Name())
}
