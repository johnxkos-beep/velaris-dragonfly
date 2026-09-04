package news

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// /news — form-based repeating-announcement setup
// ---------------------------------------------------------------------
//
// Port of NewsCommand::openForm's CustomForm: three inputs (message,
// repeat duration in minutes, delay between repeats in seconds), in the
// same order. See countdown/form.go's setupForm doc comment for how
// Dragonfly's reflection-based custom forms work (exported fields ARE
// the form elements, in declaration order) — this follows the exact
// same shape.

// setupForm is both the form's element layout and its Submittable.
type setupForm struct {
	Message         form.Input
	DurationMinutes form.Input
	DelaySeconds    form.Input
}

func (f setupForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}

	message := strings.TrimSpace(f.Message.Value())
	if message == "" {
		p.Message("§cAnnouncement cancelled - message can't be empty.")
		return
	}

	durationMinutes, err1 := strconv.ParseFloat(strings.TrimSpace(f.DurationMinutes.Value()), 64)
	delaySeconds, err2 := strconv.ParseFloat(strings.TrimSpace(f.DelaySeconds.Value()), 64)
	if err1 != nil || err2 != nil || durationMinutes <= 0 || delaySeconds <= 0 {
		p.Message("§cDuration and delay both need to be positive numbers.")
		return
	}

	intervalTicks := int(delaySeconds*20 + 0.5)
	if intervalTicks < 1 {
		intervalTicks = 1
	}
	totalDurationTicks := int(durationMinutes*60*20 + 0.5)

	StartRepeating(message, intervalTicks, totalDurationTicks)
	p.Messagef("§aAnnouncement scheduled: repeating every %ds for %g minute(s).", int(delaySeconds), durationMinutes)
}

// Close is called if the player dismisses the form without submitting —
// the original had no Close handling either, so this is a silent no-op
// to match.
func (setupForm) Close(submitter form.Submitter, tx *world.Tx) {}

// sendSetupForm opens the /news setup form for p — port of
// NewsCommand::openForm.
func sendSetupForm(p *player.Player) {
	f := setupForm{
		Message:         form.NewInput("Message", "e.g. Drop party at spawn in 5 minutes!", ""),
		DurationMinutes: form.NewInput("Repeat for how long? (minutes)", "e.g. 10", "10"),
		DelaySeconds:    form.NewInput("Delay between each repeat (seconds)", "e.g. 60", "60"),
	}
	p.SendForm(form.New(f, "News Announcement"))
}
