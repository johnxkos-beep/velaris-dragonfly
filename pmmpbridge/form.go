package dragonfly

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// FormMapperFor builds a FormMapper that converts PMMP form JSON into
// real Dragonfly forms, and forwards whatever the player does with them
// back to PHP via client.FormResponse.
//
// SUPPORTED: PMMP "simple" forms (type: "form" — a title/body plus a
// list of buttons, e.g. an ability picker or team menu) and "modal"
// forms (title/body plus two buttons, e.g. a yes/no confirmation).
// Both are built as a Dragonfly Menu, since Menu buttons are plain
// runtime data (no compile-time struct needed per button).
//
// NOT SUPPORTED: PMMP "custom_form" (sliders, toggles, dropdowns, text
// inputs). Dragonfly's custom-form API requires a distinct, named Go
// struct type written at compile time for every exact combination of
// fields a form might have — Go doesn't allow constructing a new type
// with methods at runtime, so there's no way to build one dynamically
// from arbitrary PHP-defined JSON. A plugin command that opens a
// custom_form will get a logged error and nothing will appear
// client-side; simple/modal forms from the same plugin still work fine.
func FormMapperFor(rt *Runtime) FormMapper {
	return func(id int, raw json.RawMessage) (form.Form, error) {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return nil, fmt.Errorf("pmmp form: invalid json: %w", err)
		}
		switch head.Type {
		case "form":
			return newPMMPMenuForm(rt, id, raw)
		case "modal":
			return newPMMPModalAsMenu(rt, id, raw)
		case "custom_form":
			return nil, fmt.Errorf("pmmp form: custom_form isn't supported — Dragonfly needs a compile-time struct per form shape, which can't be built dynamically from PHP-defined JSON")
		default:
			return nil, fmt.Errorf("pmmp form: unknown form type %q", head.Type)
		}
	}
}

type pmmpButtonImage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type pmmpButton struct {
	Text  string           `json:"text"`
	Image *pmmpButtonImage `json:"image,omitempty"`
}

type pmmpSimpleFormPayload struct {
	Title   string       `json:"title"`
	Content string       `json:"content"`
	Buttons []pmmpButton `json:"buttons"`
}

type pmmpModalFormPayload struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Button1 string `json:"button1"`
	Button2 string `json:"button2"`
}

// pmmpMenuForm is the Submittable behind a Dragonfly Menu built from a
// PMMP form. buttons is stored so Submit can work out which button
// index the player pressed (PMMP identifies choices by index, Dragonfly
// gives back the pressed form.Button itself).
type pmmpMenuForm struct {
	rt      *Runtime
	formID  int
	buttons []pmmpButton
}

func newPMMPMenuForm(rt *Runtime, id int, raw json.RawMessage) (form.Form, error) {
	var payload pmmpSimpleFormPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("pmmp form: invalid simple form json: %w", err)
	}
	return buildMenu(rt, id, payload.Title, payload.Content, payload.Buttons), nil
}

// newPMMPModalAsMenu represents a PMMP modal (yes/no) form as a
// two-button Menu instead. Dragonfly does have a separate Modal form
// type, but its exact API isn't something we've verified against this
// server's Dragonfly version yet, whereas Menu is already proven
// working elsewhere in this codebase (see rankforms/). Two clearly
// labelled buttons is a reasonable stand-in visually.
func newPMMPModalAsMenu(rt *Runtime, id int, raw json.RawMessage) (form.Form, error) {
	var payload pmmpModalFormPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("pmmp form: invalid modal form json: %w", err)
	}
	buttons := []pmmpButton{{Text: payload.Button1}, {Text: payload.Button2}}
	return buildMenu(rt, id, payload.Title, payload.Content, buttons), nil
}

func buildMenu(rt *Runtime, id int, title, body string, buttons []pmmpButton) form.Menu {
	handler := pmmpMenuForm{rt: rt, formID: id, buttons: buttons}
	dfButtons := make([]form.Button, len(buttons))
	for i, b := range buttons {
		image := ""
		if b.Image != nil {
			image = b.Image.Data
		}
		dfButtons[i] = form.NewButton(b.Text, image)
	}
	menu := form.NewMenu(handler, title).WithButtons(dfButtons...)
	if body != "" {
		menu = menu.WithBody(body)
	}
	return menu
}

func (f pmmpMenuForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	index := -1
	for i, b := range f.buttons {
		if b.Text == pressed.Text {
			index = i
			break
		}
	}
	f.respond(submitter, index)
}

func (f pmmpMenuForm) respond(submitter form.Submitter, buttonIndex int) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	callCtx, cancel := f.rt.context()
	defer cancel()

	_, actions, err := f.rt.client.FormResponse(callCtx, p.UUID().String(), f.formID, buttonIndex)
	if err != nil {
		f.rt.report(err)
		return
	}
	f.rt.applyActions(callCtx, actions)
}
