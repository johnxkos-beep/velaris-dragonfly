package chatcooldown

import (
	"strconv"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// /cooldown — form-based config editor
// ---------------------------------------------------------------------
//
// See the doc comment on knockback's configForm (knockback/form.go) for
// how Dragonfly's reflection-based custom forms work: the exported
// fields ARE the form elements, in declaration order.
//
// Port note: the original CooldownForm pre-filled the input's default
// text with the string "Current cooldown: 3" (a human-readable sentence,
// not a bare number) — submitting the form unchanged would fail its own
// is_numeric validation. This version pre-fills the bare current value
// instead ("3"), so re-submitting without changes just re-saves the same
// number, and puts the "current value" context in the label instead.

// configForm is both the form's element layout and its Submittable.
type configForm struct {
	cfg *Config // unexported: not reflected into a form element

	Seconds form.Input
}

func (f configForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}

	seconds, err := strconv.Atoi(f.Seconds.Value())
	if err != nil || seconds < 0 {
		p.Message("§cYou must type a valid number!")
		return
	}

	current := f.cfg.Snapshot()
	if err := f.cfg.Save(settings{
		Seconds: seconds,
		Message: current.Message,
	}); err != nil {
		p.Message("§cFailed to save config: " + err.Error())
		return
	}

	p.Messagef("§aYou have set the cooldown to %d seconds!", seconds)
}

// Close is called if the player dismisses the form without submitting.
func (configForm) Close(submitter form.Submitter, tx *world.Tx) {
	if p, ok := submitter.(*player.Player); ok {
		p.Message("§7Chat cooldown editor closed, no changes were made.")
	}
}

// sendConfigForm opens the /cooldown config editor for p, pre-filled
// with the current cooldown from cfg.
func sendConfigForm(p *player.Player, cfg *Config) {
	s := cfg.Snapshot()

	f := configForm{
		cfg:     cfg,
		Seconds: form.NewInput("Time in seconds (current: "+strconv.Itoa(s.Seconds)+", set to 0 to disable)", "", strconv.Itoa(s.Seconds)),
	}
	p.SendForm(form.New(f, "Update the cooldown"))
}
