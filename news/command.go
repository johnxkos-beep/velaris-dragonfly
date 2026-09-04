package news

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is /news [message] — port of NewsCommand::execute.
//
// With a message: broadcasts it once, immediately, in chat and on the
// action bar. Without one: opens the repeating-announcement setup form
// (see form.go) if run in-game, or prints a usage hint if run from
// console — same split as the original ("the setup form needs to be
// used in-game - run this from console with a message instead").
//
// Message is cmd.Optional[string] rather than required so bare "/news"
// still parses — same trailing-string-captures-the-rest-of-the-line
// pattern already used by teams.TeamChat and schematic.Load elsewhere in
// this project (see either's doc comment).
//
// Op only (hoplite.news defaulted to "op" in the original plugin.yml),
// matching every other server-wide admin broadcast/config command in
// this codebase.
type Command struct {
	Message cmd.Optional[string] `cmd:"message"`
}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (c Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	message, has := c.Message.Load()
	if has && message != "" {
		BroadcastOnce(tx, message)
		output.Print("Announcement broadcast.")
		return
	}

	p, ok := src.(*player.Player)
	if !ok {
		output.Print("Usage: /news <message> (the setup form needs to be used in-game - run this from console with a message instead)")
		return
	}
	sendSetupForm(p)
}
