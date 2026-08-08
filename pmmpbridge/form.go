package dragonfly

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

type RawFormSubmitHandler func(formID int, response json.RawMessage, submitter form.Submitter, tx *world.Tx) error

type RawForm struct {
	id       int
	payload  json.RawMessage
	onSubmit RawFormSubmitHandler
}

func NewRawForm(id int, payload json.RawMessage, onSubmit RawFormSubmitHandler) (RawForm, error) {
	copied, err := copyJSONObject(payload)
	if err != nil {
		return RawForm{}, fmt.Errorf("invalid PMMP form payload: %w", err)
	}
	return RawForm{id: id, payload: copied, onSubmit: onSubmit}, nil
}

func RawFormMapper(onSubmit RawFormSubmitHandler) FormMapper {
	return func(id int, payload json.RawMessage) (form.Form, error) {
		return NewRawForm(id, payload, onSubmit)
	}
}

func (f RawForm) MarshalJSON() ([]byte, error) {
	return append([]byte(nil), f.payload...), nil
}

func (f RawForm) SubmitJSON(response []byte, submitter form.Submitter, tx *world.Tx) error {
	if f.onSubmit == nil {
		return nil
	}
	copied := json.RawMessage("null")
	if response != nil {
		if !json.Valid(response) {
			return fmt.Errorf("invalid PMMP form response JSON")
		}
		copied = append(json.RawMessage(nil), response...)
	}
	return f.onSubmit(f.id, copied, submitter, tx)
}

func copyJSONObject(payload json.RawMessage) (json.RawMessage, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("malformed JSON")
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, err
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("payload must be a non-empty JSON object")
	}
	formType, ok := object["type"].(string)
	if !ok || formType == "" {
		return nil, fmt.Errorf("payload missing form type")
	}
	switch formType {
	case "form", "modal", "custom_form":
	default:
		return nil, fmt.Errorf("unsupported form type %q", formType)
	}
	if formType == "custom_form" && normalizeCustomForm(object) {
		normalised, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		return normalised, nil
	}
	return append(json.RawMessage(nil), payload...), nil
}

func normalizeCustomForm(object map[string]any) bool {
	content, ok := object["content"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, raw := range content {
		element, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		elementType, _ := element["type"].(string)
		switch elementType {
		case "label":
			changed = ensureString(element, "text") || changed
		case "input":
			changed = ensureString(element, "text") || changed
			changed = ensureString(element, "placeholder") || changed
			changed = ensureString(element, "default") || changed
		case "toggle":
			changed = ensureString(element, "text") || changed
			changed = ensureBool(element, "default") || changed
		case "slider":
			changed = ensureString(element, "text") || changed
			changed = ensureNumber(element, "default", numberValue(element["min"], 0)) || changed
		case "dropdown", "step_slider":
			changed = ensureString(element, "text") || changed
			changed = ensureNumber(element, "default", 0) || changed
		}
	}
	return changed
}

func ensureString(object map[string]any, key string) bool {
	if value, ok := object[key]; ok && value != nil {
		return false
	}
	object[key] = ""
	return true
}

func ensureBool(object map[string]any, key string) bool {
	if value, ok := object[key]; ok && value != nil {
		return false
	}
	object[key] = false
	return true
}

func ensureNumber(object map[string]any, key string, fallback float64) bool {
	if value, ok := object[key]; ok && value != nil {
		return false
	}
	object[key] = fallback
	return true
}

func numberValue(value any, fallback float64) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}
