package chatcooldown

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"

	"velaris-dragonfly/state"
)

// lastChat tracks, per XUID, the time of that player's last allowed chat
// message — replaces the original plugin's per-player Session objects
// (session/Session.php). Same map-keyed-by-XUID pattern
// knockback.lastAttack uses for its own per-player cooldown.
var (
	mu       sync.Mutex
	lastChat = map[string]time.Time{}
)

// OnChat should be called from PlayerHandler.HandleChat, before the
// message is formatted/broadcast. It reports whether p is allowed to
// send this chat message right now — port of CooldownListener::onChat —
// and, if not, the already-colored, ready-to-send deny message with
// "(time)" substituted for the whole seconds remaining. If allowed, it
// also records this as p's new last-chat time (port of
// Session::canChat's side effect of stamping last_chat_time).
func OnChat(p *player.Player) (allowed bool, denyMessage string) {
	if Cfg == nil {
		return true, ""
	}
	s := Cfg.Snapshot()
	if s.Seconds <= 0 {
		return true, ""
	}

	// Ops bypass the cooldown entirely — port of "chatcooldown.bypass",
	// which defaulted to op.
	if state.Ops.IsOp(p.XUID()) {
		return true, ""
	}

	id := p.XUID()
	cooldown := time.Duration(s.Seconds) * time.Second
	now := time.Now()

	mu.Lock()
	defer mu.Unlock()

	if last, ok := lastChat[id]; ok {
		if elapsed := now.Sub(last); elapsed < cooldown {
			remaining := int((cooldown - elapsed) / time.Second)
			if (cooldown-elapsed)%time.Second != 0 {
				remaining++ // round up, same as the PHP int cast rounding down time() diffs would never show "0 seconds left" while still blocked
			}
			if remaining < 1 {
				remaining = 1
			}
			return false, strings.ReplaceAll(s.Message, "(time)", strconv.Itoa(remaining))
		}
	}
	lastChat[id] = now
	return true, ""
}

// ClearPlayer removes any cooldown state tracked for the given XUID. Call
// this from PlayerHandler.HandleQuit so this package doesn't slowly leak
// memory over a long-running server — same pattern as
// knockback.ClearPlayer / legendary.ClearPlayer.
func ClearPlayer(xuid string) {
	mu.Lock()
	delete(lastChat, xuid)
	mu.Unlock()
}
