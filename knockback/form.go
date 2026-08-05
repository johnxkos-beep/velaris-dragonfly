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
// This mirrors the original plugin's KBForm element-for-element and in
// the same order, so the element indices below line up with that PHP
// file's jsonSerialize() array. Follows the same construction style as
// rankforms.go (form.New(...).WithElements(...), p.SendForm(...)).
//
// NOTE: form.New / .WithElements / form.NewLabel / form.NewInput /
// form.NewToggle and the Input/Toggle .Value() accessors are my best read
// of the current Dragonfly form API — rankforms.go in this project already
// confirms form.NewMenu/.WithBody/.WithButtons and the Button.Text field
// work, but I don't have a confirmed reference for the plain (non-menu)
// custom form in this codebase. If any of these names are off, paste the
// compiler error and it's a quick fix.

// configForm is the Submittable behind /kb.
type configForm struct {
	cfg *Config
}

// element indices, matching the original KBForm array layout:
//
//	0  label
//	1  input  Horizontal KB
//	2  input  Vertical KB
//	3  input  Height Limiter
//	4  input  Attack Cooldown (ticks)
//	5  label  "-- Projectiles --"
//	6  toggle Projectile Cooldown Enabled
//	7  input  Projectile Cooldown (seconds)
//	8  input  Projectile Cooldown Message
//	9  label  "-- Sounds --"
//	10 toggle Remove Vanilla Hit Sound (unenforced, see config.go)
//	11 toggle Ding on Projectile Hit
//	12 input  Ding Pitch (0-24)
const (
	idxHorizontal = 1
	idxVertical   = 2
	idxHeightCap  = 3
	idxAtkCD      = 4
	idxProjEnable = 6
	idxProjSecs   = 7
	idxProjMsg    = 8
	idxRemoveHit  = 10
	idxDingEnable = 11
	idxDingPitch  = 12
)

func (f configForm) Submit(submitter form.Submitter, elements []form.Element, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if len(elements) < 13 {
		p.Message("§cInvalid form response.")
		return
	}

	horizontal, ok1 := elements[idxHorizontal].(form.Input)
	vertical, ok2 := elements[idxVertical].(form.Input)
	heightCap, ok3 := elements[idxHeightCap].(form.Input)
	attackCD, ok4 := elements[idxAtkCD].(form.Input)
	projEnable, ok5 := elements[idxProjEnable].(form.Toggle)
	projSecs, ok6 := elements[idxProjSecs].(form.Input)
	projMsg, ok7 := elements[idxProjMsg].(form.Input)
	removeHit, ok8 := elements[idxRemoveHit].(form.Toggle)
	dingEnable, ok9 := elements[idxDingEnable].(form.Toggle)
	dingPitch, ok10 := elements[idxDingPitch].(form.Input)

	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 && ok8 && ok9 && ok10) {
		p.Message("§cInvalid form response.")
		return
	}

	h, err := strconv.ParseFloat(horizontal.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Horizontal KB\": must be a number. No changes were saved.")
		return
	}
	v, err := strconv.ParseFloat(vertical.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Vertical KB\": must be a number. No changes were saved.")
		return
	}
	hc, err := strconv.ParseFloat(heightCap.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Height Limiter\": must be a number. No changes were saved.")
		return
	}
	ac, err := strconv.Atoi(attackCD.Value())
	if err != nil || ac < 0 {
		p.Message("§cInvalid value for \"Attack Cooldown\": must be a non-negative integer. No changes were saved.")
		return
	}
	ps, err := strconv.ParseFloat(projSecs.Value(), 64)
	if err != nil {
		p.Message("§cInvalid value for \"Projectile Cooldown (seconds)\": must be a number. No changes were saved.")
		return
	}
	dp, err := strconv.Atoi(dingPitch.Value())
	if err != nil {
		p.Message("§cInvalid value for \"Ding Pitch\": must be a number. No changes were saved.")
		return
	}

	if err := f.cfg.Save(settings{
		Horizontal:                h,
		Vertical:                  v,
		HeightLimit:               hc,
		AttackCooldownTicks:       ac,
		ProjectileCooldownEnabled: projEnable.Value(),
		ProjectileCooldownSeconds: ps,
		ProjectileCooldownMessage: projMsg.Value(),
		RemoveHitSound:            removeHit.Value(),
		DingEnabled:               dingEnable.Value(),
		DingPitch:                 dp,
	}); err != nil {
		p.Message("§cFailed to save config: " + err.Error())
		return
	}

	p.Message("§aKB configuration updated successfully!")
}

// Close is called if the player dismisses the form without submitting.
// Not every Dragonfly form version supports this — harmless either way if
// it's unused.
func (configForm) Close(submitter form.Submitter, tx *world.Tx) {
	if p, ok := submitter.(*player.Player); ok {
		p.Message("§7KB config editor closed, no changes were made.")
	}
}

// sendConfigForm opens the /kb config editor for p, pre-filled with the
// current values from cfg.
func sendConfigForm(p *player.Player, cfg *Config) {
	s := cfg.Snapshot()

	f := form.New(configForm{cfg: cfg}, "KB Config Editor").WithElements(
		form.NewLabel("Edit values below, then hit Submit to save."),
		form.NewInput("Horizontal KB", "0.4", fmt.Sprintf("%v", s.Horizontal)),
		form.NewInput("Vertical KB", "0.4", fmt.Sprintf("%v", s.Vertical)),
		form.NewInput("Height Limiter (max upward velocity)", "0.4", fmt.Sprintf("%v", s.HeightLimit)),
		form.NewInput("Attack Cooldown (ticks, 20 = 1s)", "10", fmt.Sprintf("%d", s.AttackCooldownTicks)),
		form.NewLabel("-- Projectiles --"),
		form.NewToggle("Projectile Cooldown Enabled", s.ProjectileCooldownEnabled),
		form.NewInput("Projectile Cooldown (seconds)", "2.5", fmt.Sprintf("%v", s.ProjectileCooldownSeconds)),
		form.NewInput("Projectile Cooldown Message", "§cWait before shooting again!", s.ProjectileCooldownMessage),
		form.NewLabel("-- Sounds --"),
		form.NewToggle("Remove Vanilla Hit Sound (currently unenforced)", s.RemoveHitSound),
		form.NewToggle("Ding on Projectile Hit", s.DingEnabled),
		form.NewInput("Ding Pitch (0-24)", "12", fmt.Sprintf("%d", s.DingPitch)),
	)
	p.SendForm(f)
}
