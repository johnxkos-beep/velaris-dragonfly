// Package state holds the server-wide shared state (loaded ranks, ops,
// bans, the online player list, and the *server.Server itself) along with
// helpers that read it. Everything here is set once in main() before the
// server starts accepting players, then read from commands, forms, and
// player event handlers across the other packages.
package state

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/opsbans"
	"velaris-dragonfly/ranks"
)

// Ops, Bans, Ranks, RankDefs, and Server are set once in main() before the
// server starts accepting players, then read by command Allow/Run methods
// and form handlers throughout the other packages.
var (
	Ops      *opsbans.OpSet
	Bans     *opsbans.BanSet
	Ranks    *ranks.RankSet
	RankDefs *ranks.RankDefSet
	Server   *server.Server
)

// CoordsMu/CoordsState track, per XUID, whether that player currently has
// coordinates enabled, since /coords toggles it. Coordinates are shown by
// default on join (see main()).
var (
	CoordsMu    sync.Mutex
	CoordsState = map[string]bool{}
)

// ---------------------------------------------------------------------
// Online player tracking (by name, for commands that target other players
// by name instead of a selector — e.g. /kick, /ban, /op, /tpto)
// ---------------------------------------------------------------------

var (
	onlineMu      sync.RWMutex
	onlinePlayers = map[string]*player.Player{} // lowercase name -> player
)

// TrackJoin records a player as online. Call this once, on join.
func TrackJoin(p *player.Player) {
	onlineMu.Lock()
	onlinePlayers[lower(p.Name())] = p
	onlineMu.Unlock()
}

// TrackQuit removes a player from the online list. Call this on quit.
func TrackQuit(p *player.Player) {
	onlineMu.Lock()
	delete(onlinePlayers, lower(p.Name()))
	onlineMu.Unlock()
}

// FindOnline looks up a currently-online player by name.
func FindOnline(name string) (*player.Player, bool) {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	p, ok := onlinePlayers[lower(name)]
	return p, ok
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// MatchOnlineGreedy tries to match the longest possible prefix of args
// against an online player's name, so console commands work with
// multi-word names without needing quotes — e.g. "kick Velaris Founder
// being rude" correctly splits into name="Velaris Founder" and
// reason="being rude" by checking which online player it actually matches.
// Returns nil if no online player matches any prefix of args.
func MatchOnlineGreedy(args []string) (*player.Player, []string) {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	for n := len(args); n >= 1; n-- {
		candidate := lower(strings.Join(args[:n], " "))
		if p, ok := onlinePlayers[candidate]; ok {
			return p, args[n:]
		}
	}
	return nil, nil
}

// FindAndActOnline safely locates an online player by name (matching the
// longest possible prefix of args, so multi-word names work without
// quotes) and runs fn on it from within the transaction srv.Players opens
// for that player. This is required for anything that touches world/session
// state — e.g. Disconnect — since the *player.Player pointers cached in
// onlinePlayers are only safe to read simple fields from, not to mutate
// through, once execution is outside of their owning world transaction.
// Calling Disconnect directly on a cached pointer panics on dragonfly's
// internal tick goroutine, which no recover() in runConsole can catch, and
// takes the whole process down.
func FindAndActOnline(srv *server.Server, args []string, fn func(p *player.Player, rest []string)) bool {
	for n := len(args); n >= 1; n-- {
		name := lower(strings.Join(args[:n], " "))
		for p := range srv.Players(nil) {
			if lower(p.Name()) == name {
				fn(p, args[n:])
				return true
			}
		}
	}
	return false
}

// FindOnlineTx safely resolves an online player by name using the
// transaction tx belongs to, so the *player.Player returned is safe to
// mutate through — including calling Disconnect. This is required for
// anything (kick, ban, /rank's forms) that touches player/world state; the
// pointers cached by TrackJoin/FindOnline are only safe to read simple
// fields from once you're outside the transaction that owns them. See
// FindAndActOnline's doc comment (used by the console) for the same
// concern from a goroutine with no tx at all.
func FindOnlineTx(tx *world.Tx, name string) (*player.Player, bool) {
	target := lower(name)
	for p := range Server.Players(tx) {
		if lower(p.Name()) == target {
			return p, true
		}
	}
	return nil, false
}

// IsOpSource reports whether the command source is allowed to run
// operator-only commands. Non-player sources (the server console) are
// always allowed; players must be on the op list.
func IsOpSource(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	if !ok {
		return true
	}
	return Ops.IsOp(p.XUID())
}
