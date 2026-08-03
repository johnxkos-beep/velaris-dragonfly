// Package cmds holds every custom command for the server. Each command is
// a struct implementing cmd.Runnable, registered in RegisterAll. This
// replaces PMMP's Command classes — add a new file per command (or group of
// related commands) as you port things like /builder, /track, /koth, etc.
package cmds

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Ping is a minimal example command: /ping just replies "Pong!". Use this
// as the template for new commands — copy the shape, rename, add fields for
// arguments if the command needs them.
type Ping struct{}

// Run is called when a player executes /ping. src is normally a *player.Player,
// but commands can also be run from console depending on Source.
func (Ping) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if p, ok := src.(*player.Player); ok {
		p.Message("§aPong!")
		return
	}
	output.Print("Pong!")
}

// RegisterAll registers every command in this package. Call this once from
// main.go before the server starts accepting players.
//
// NOTE: verify cmd.New's exact signature against the version of Dragonfly
// pinned in go.mod (https://pkg.go.dev/github.com/df-mc/dragonfly/server/cmd)
// before relying on this — command APIs are one of the areas that shifts
// between Dragonfly versions.
func RegisterAll() {
	cmd.Register(cmd.New("ping", "Replies with Pong.", nil, Ping{}))
}
