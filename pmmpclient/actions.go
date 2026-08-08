package pmmpcompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrUnknownAction     = errors.New("unknown pmmpcompat action")
	ErrMissingPlayer     = errors.New("pmmpcompat action references an unknown player")
	ErrMalformedAction   = errors.New("malformed pmmpcompat action")
	ErrUnsupportedAction = errors.New("unsupported pmmpcompat action")
)

type TargetResolver interface {
	Player(uuid string) (PlayerTarget, bool)
	Server() ServerTarget
}

type PlayerTarget interface {
	SendMessage(ctx context.Context, message string) error
	SendPopup(ctx context.Context, message string) error
	SendTip(ctx context.Context, message string) error
	SendActionBar(ctx context.Context, message string) error
	SendTitle(ctx context.Context, title, subtitle string) error
	SetTitleDuration(ctx context.Context, fadeIn, stay, fadeOut int) error
	ResetTitles(ctx context.Context) error
	RemoveTitles(ctx context.Context) error
	Teleport(ctx context.Context, position Position) error
	Kick(ctx context.Context, reason string) error
	Transfer(ctx context.Context, address string, port int, message string) error
	SendForm(ctx context.Context, formID int, form json.RawMessage) error
	SetGamemode(ctx context.Context, gamemode string) error
	SetHealth(ctx context.Context, health, maxHealth float64) error
	SetExperience(ctx context.Context, level int, progress float64) error
	SetAllowFlight(ctx context.Context, value bool) error
	SetFlying(ctx context.Context, value bool) error
	SetFlightSpeed(ctx context.Context, speed float64) error
	SetViewDistance(ctx context.Context, distance int) error
	SetInventoryItem(ctx context.Context, slot int, item InventoryItem) error
	ClearInventorySlot(ctx context.Context, slot int) error
	ClearInventory(ctx context.Context) error
}

type ServerTarget interface {
	BroadcastMessage(ctx context.Context, message string) error
}

type UnsupportedPlayerTarget struct{}

func (UnsupportedPlayerTarget) SendMessage(context.Context, string) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SendPopup(context.Context, string) error { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) SendTip(context.Context, string) error   { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) SendActionBar(context.Context, string) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SendTitle(context.Context, string, string) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetTitleDuration(context.Context, int, int, int) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) ResetTitles(context.Context) error        { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) RemoveTitles(context.Context) error       { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) Teleport(context.Context, Position) error { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) Kick(context.Context, string) error       { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) Transfer(context.Context, string, int, string) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SendForm(context.Context, int, json.RawMessage) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetGamemode(context.Context, string) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetHealth(context.Context, float64, float64) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetExperience(context.Context, int, float64) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetAllowFlight(context.Context, bool) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetFlying(context.Context, bool) error { return ErrUnsupportedAction }
func (UnsupportedPlayerTarget) SetFlightSpeed(context.Context, float64) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetViewDistance(context.Context, int) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) SetInventoryItem(context.Context, int, InventoryItem) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) ClearInventorySlot(context.Context, int) error {
	return ErrUnsupportedAction
}
func (UnsupportedPlayerTarget) ClearInventory(context.Context) error { return ErrUnsupportedAction }

type UnsupportedServerTarget struct{}

func (UnsupportedServerTarget) BroadcastMessage(context.Context, string) error {
	return ErrUnsupportedAction
}

func ApplyActions(ctx context.Context, resolver TargetResolver, actions []Action) error {
	for i, action := range actions {
		if err := ApplyAction(ctx, resolver, action); err != nil {
			return fmt.Errorf("apply action %d (%s): %w", i, action.Type, err)
		}
	}
	return nil
}

func ApplyAction(ctx context.Context, resolver TargetResolver, action Action) error {
	switch action.Type {
	case "server.broadcast_message":
		server := resolver.Server()
		if server == nil {
			return fmt.Errorf("%w: server target is nil", ErrMissingPlayer)
		}
		return server.BroadcastMessage(ctx, action.Message)
	case "player.send_message":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendMessage(ctx, action.Message) })
	case "player.send_popup":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendPopup(ctx, action.Message) })
	case "player.send_tip":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendTip(ctx, action.Message) })
	case "player.send_actionbar":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendActionBar(ctx, action.Message) })
	case "player.send_title":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendTitle(ctx, action.Title, action.Subtitle) })
	case "player.set_title_duration":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error {
			return player.SetTitleDuration(ctx, action.FadeIn, action.Stay, action.FadeOut)
		})
	case "player.reset_titles":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.ResetTitles(ctx) })
	case "player.remove_titles":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.RemoveTitles(ctx) })
	case "player.teleport":
		if action.Position == nil {
			return fmt.Errorf("%w: teleport missing position", ErrMalformedAction)
		}
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.Teleport(ctx, *action.Position) })
	case "player.kick":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.Kick(ctx, action.Reason) })
	case "player.transfer":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error {
			return player.Transfer(ctx, action.Address, action.Port, action.Message)
		})
	case "player.send_form":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SendForm(ctx, action.FormID, action.Form) })
	case "player.set_gamemode":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetGamemode(ctx, action.Gamemode) })
	case "player.set_health":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetHealth(ctx, action.Health, action.MaxHealth) })
	case "player.set_experience":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error {
			return player.SetExperience(ctx, action.XPLevel, action.XPProgress)
		})
	case "player.set_allow_flight":
		value, err := boolValue(action)
		if err != nil {
			return err
		}
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetAllowFlight(ctx, value) })
	case "player.set_flying":
		value, err := boolValue(action)
		if err != nil {
			return err
		}
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetFlying(ctx, value) })
	case "player.set_flight_speed":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetFlightSpeed(ctx, action.Speed) })
	case "player.set_view_distance":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetViewDistance(ctx, action.Distance) })
	case "player.inventory.set_item":
		item, err := decodeInventoryItem(action.Item)
		if err != nil {
			return err
		}
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.SetInventoryItem(ctx, action.Slot, item) })
	case "player.inventory.clear_slot":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.ClearInventorySlot(ctx, action.Slot) })
	case "player.inventory.clear":
		return withPlayer(ctx, resolver, action, func(player PlayerTarget) error { return player.ClearInventory(ctx) })
	default:
		return fmt.Errorf("%w: %s", ErrUnknownAction, action.Type)
	}
}

func withPlayer(ctx context.Context, resolver TargetResolver, action Action, f func(PlayerTarget) error) error {
	if action.UUID == "" {
		return fmt.Errorf("%w: missing uuid", ErrMalformedAction)
	}
	player, ok := resolver.Player(action.UUID)
	if !ok || player == nil {
		return fmt.Errorf("%w: %s", ErrMissingPlayer, action.UUID)
	}
	return f(player)
}

func boolValue(action Action) (bool, error) {
	if action.Value == nil {
		return false, fmt.Errorf("%w: %s missing value", ErrMalformedAction, action.Type)
	}
	return *action.Value, nil
}

func decodeInventoryItem(raw json.RawMessage) (InventoryItem, error) {
	if len(raw) == 0 {
		return InventoryItem{}, fmt.Errorf("%w: inventory item missing", ErrMalformedAction)
	}
	var item InventoryItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return InventoryItem{}, fmt.Errorf("%w: decode inventory item: %v", ErrMalformedAction, err)
	}
	if item.TypeID == "" {
		return InventoryItem{}, fmt.Errorf("%w: inventory item missing type_id", ErrMalformedAction)
	}
	return item, nil
}
