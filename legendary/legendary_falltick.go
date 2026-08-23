package legendary

import (
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
)

// StartFallTicker runs TickGolemFall for every online player, once a tick
// (20/sec), for as long as the server runs — this is what actually drives
// Golem Hammer's fall-damage bonus (see abilities.go's OnHurt, which reads
// the peak height this records).
//
// NOT WIRED IN YET — add one line to main() after srv.Listen():
//
//	go legendary.StartFallTicker(srv)
//
// UNVERIFIED: srv.World().Exec(...) is my best guess at how to get a valid
// *world.Tx from a background goroutine outside of any handler/command
// callback in this Dragonfly version — nothing else in this repo does
// that today (every other Tx use in this codebase comes from a handler or
// command that Dragonfly already handed one to). If srv.World() or .Exec
// don't exist under those names, paste the compiler error and it's a
// one-line fix — the logic in TickGolemFall itself doesn't depend on
// exactly how the Tx was obtained.
func StartFallTicker(srv *server.Server) {
	t := time.NewTicker(50 * time.Millisecond) // 1 tick
	defer t.Stop()
	for range t.C {
		srv.World().Exec(func(tx *world.Tx) {
			for p := range srv.Players(tx) {
				TickGolemFall(p)
			}
		})
	}
}
