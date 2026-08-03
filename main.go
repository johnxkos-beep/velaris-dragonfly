package main

import (
	"log/slog"
	"os"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player/chat"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	conf, err := server.DefaultConfig().Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	// broadcast player chat/join/quit messages
	conf.Allower = nil
	srv := conf.New()
	srv.CloseOnProgramEnd()

	chat.Global.Subscribe(chat.StdoutSubscriber{})

	srv.Listen()
	for p := range srv.Accept() {
		_ = p // handle joining players here later
	}
}
