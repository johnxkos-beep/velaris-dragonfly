package knockback

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// /kb — form-based config editor
// ---------------------------------------------------------------------
//
// Dragonfly's custom form works differently than I first assumed: the
// exported fields of a struct ARE the form elements (in declaration
// order), populated via reflection before Submit is called — there's no
// separate elements slice or .WithElements(...) call. This matches how
// the cmd package works elsewhere in this project (exported fields with
// `cmd:"..."` tags), just without the tag. The unexported cfg field below
// is ignored by that reflection and is just a normal Go field we read in
// Submit.
//
// Field order below matches the original PHP KBForm's element order.

// configForm is both the form's element layout and its Submittable.
type configForm struct {
	cfg *Config // unexported: not reflected into a form element

	Info       form.Label
	Horizontal form.Input
	Vertical   form.Input
	HeightCap  form.Input
	AttackCD   form.Input

	ProjHeader  form.Label
	ProjEnabled form.Toggle
	ProjSeconds form.Input
	ProjMessage form.Input

	SoundHeader form.Label
	RemoveHit   form.Toggle
	DingEnabled form.Toggle
	DingPitch   form.Input
}

func (f configForm) Submit(submitter form.Submitter, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}

	h, err := strconv.ParseFloat(f.Horizontal.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Horizontal KB\": must be a number. No changes were saved.")
		return
	}
	v, err := strconv.ParseFloat(f.Vertical.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Vertical KB\": must be a number. No changes were saved.")
		return
	}
	hc, err := strconv.ParseFloat(f.HeightCap.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Height Limiter\": must be a number. No changes were saved.")
		return
	}
	ac, err := strconv.Atoi(f.AttackCD.Value())
	if err != nil || ac < 0 {
		p.Message("§cInvalid value for \"Attack Cooldown\": must be a non-negative integer. No changes were saved.")
		return
	}
	ps, err := strconv.ParseFloat(f.ProjSeconds.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Projectile Cooldown (seconds)\": must be a number. No changes were saved.")
		return
	}
	dp, err := strconv.Atoi(f.DingPitch.Value())
	if err != nil {
		p.Message("§cInvalid value for \"Ding Pitch\": must be a number. No changes were saved.")
		return
	}

	if err := f.cfg.Save(settings{
		Horizontal:                h,
		Vertical:                  v,
		HeightLimit:               hc,
		AttackCooldownTicks:       ac,
		ProjectileCooldownEnabled: f.ProjEnabled.Value(),
		ProjectileCooldownSeconds: ps,
		ProjectileCooldownMessage: f.ProjMessage.Value(),
		RemoveHitSound:            f.RemoveHit.Value(),
		DingEnabled:               f.DingEnabled.Value(),
		DingPitch:                 dp,
	}); err != nil {
		p.Message("§cFailed to save config: " + err.Error())
		return
	}

	p.Message("§aKB configuration updated successfully!")
}

// Close is called if the player dismisses the form without submitting.
// Harmless if this version of the form package doesn't call it.
func (configForm) Close(submitter form.Submitter, tx *world.Tx) {
	if p, ok := submitter.(*player.Player); ok {
		p.Message("§7KB config editor closed, no changes were made.")
	}
}

// sendConfigForm opens the /kb config editor for p, pre-filled with the
// current values from cfg.
func sendConfigForm(p *player.Player, cfg *Config) {
	s := cfg.Snapshot()

	f := configForm{
		cfg: cfg,

		Info:       form.NewLabel("Edit values below, then hit Submit to save."),
		Horizontal: form.NewInput("Horizontal KB", "0.4", fmt.Sprintf("%v", s.Horizontal)),
		Vertical:   form.NewInput("Vertical KB", "0.4", fmt.Sprintf("%v", s.Vertical)),
		HeightCap:  form.NewInput("Height Limiter (max upward velocity)", "0.4", fmt.Sprintf("%v", s.HeightLimit)),
		AttackCD:   form.NewInput("Attack Cooldown (ticks, 20 = 1s)", "10", fmt.Sprintf("%d", s.AttackCooldownTicks)),

		ProjHeader:  form.NewLabel("-- Projectiles --"),
		ProjEnabled: form.NewToggle("Projectile Cooldown Enabled", s.ProjectileCooldownEnabled),
		ProjSeconds: form.NewInput("Projectile Cooldown (seconds)", "2.5", fmt.Sprintf("%v", s.ProjectileCooldownSeconds)),
		ProjMessage: form.NewInput("Projectile Cooldown Message", "§cWait before shooting again!", s.ProjectileCooldownMessage),

		SoundHeader: form.NewLabel("-- Sounds --"),
		RemoveHit:   form.NewToggle("Remove Vanilla Hit Sound (currently unenforced)", s.RemoveHitSound),
		DingEnabled: form.NewToggle("Ding on Projectile Hit", s.DingEnabled),
		DingPitch:   form.NewInput("Ding Pitch (0-24)", "12", fmt.Sprintf("%d", s.DingPitch)),
	}
	p.SendForm(form.New(f, "KB Config Editor"))
}
