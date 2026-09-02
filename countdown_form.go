package countdown

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// /count — form-based countdown setup
// ---------------------------------------------------------------------
//
// See the doc comment on knockback's configForm (knockback/form.go) for
// how Dragonfly's reflection-based custom forms work: the exported
// fields ARE the form elements, in declaration order. Field order here
// matches the original CountForm's two inputs exactly (message, then
// seconds).

// setupForm is both the form's element layout and its Submittable —
// port of CountForm.php.
type setupForm struct {
	Message form.Input
	Seconds form.Input
}

func (f setupForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}

	message := strings.TrimSpace(f.Message.Value())
	if message == "" {
		message = "Grace Period Ends In"
	}

	seconds, err := strconv.Atoi(strings.TrimSpace(f.Seconds.Value()))
	if err != nil || seconds <= 0 {
		p.Message("§cInvalid time - please enter a whole number of seconds greater than 0.")
		return
	}

	Start(message, seconds)
}

// Close is called if the player dismisses the form without submitting —
// the original had no Close handling, so this is a silent no-op to
// match.
func (setupForm) Close(submitter form.Submitter, tx *world.Tx) {}

// sendSetupForm opens the /count setup form for p.
func sendSetupForm(p *player.Player) {
	f := setupForm{
		Message: form.NewInput("Message", "Grace Period Ends In", ""),
		Seconds: form.NewInput("Time (in seconds)", "e.g. 1500", ""),
	}
	p.SendForm(form.New(f, "Countdown Setup"))
}
