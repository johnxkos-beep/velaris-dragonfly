package main

import (
	"log/slog"
	"os"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player/chat"

	"velaris-dragonfly/internal/cmds"
	"velaris-dragonfly/internal/handler"
	"velaris-dragonfly/internal/rank"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	// Use Pterodactyl's SERVER_PORT env var if set, so the listen address
	// always matches whatever allocation is assigned in the panel.
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "19132"
	}

	userConf := server.DefaultConfig()
	userConf.Network.Address = ":" + port
	userConf.Server.Name = "Velaris DragonFly"

	conf, err := userConf.Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	// broadcast player chat/join/quit messages
	conf.Allower = nil
	srv := conf.New()
	srv.CloseOnProgramEnd()

	chat.Global.Subscribe(chat.StdoutSubscriber{})

	// Register all commands defined in internal/cmds before the server
	// starts accepting players.
	cmds.RegisterAll()

	// Load rank data from disk (players/ranks.json). If the file does not
	// exist yet, an empty rank set is created.
	ranks, err := rank.Load("ranks.json")
	if err != nil {
		log.Error("failed to load ranks", "error", err.Error())
		os.Exit(1)
	}

	srv.Listen()
	for p := range srv.Accept() {
		// Attach our custom Handler to every player that joins. This is the
		// Dragonfly equivalent of registering PMMP event listeners — every
		// join/quit/chat/damage/etc hook for this player now routes through
		// handler.PlayerHandler.
		p.Handle(handler.New(p, ranks, log))

		p.Message("§aWelcome to Velaris DragonFly!")
	}
}
