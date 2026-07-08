// BedrockForge — a MITM proxy mod loader for Minecraft: Bedrock Edition.
//
// The proxy sits between the game client and a remote server. Mods can
// inspect, drop and inject packets in both directions, and register chat
// commands (messages starting with ".").
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/aodalt03-a11y/BedrockForge/proxy/core"
	"github.com/aodalt03-a11y/BedrockForge/proxy/mods/litematica"
	"github.com/sandertv/gophertunnel/minecraft"
)

type config struct {
	Server string `json:"server"`
	Listen string `json:"listen"`
}

func main() {
	log.SetFlags(log.Ltime)
	authOnly := flag.Bool("auth-only", false, "run Xbox Live device auth, save token.json and exit")
	flag.Parse()

	if *authOnly {
		runAuth()
		return
	}

	cfg := loadConfig()
	if cfg.Server == "" {
		log.Fatal("config.json: \"server\" must be set")
	}
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:19132"
	}

	src, err := tokenSource()
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	mods := []core.Mod{litematica.New()}

	// Surface gophertunnel's internal handshake/packet errors to stdout so
	// they appear in the app log; by default they are discarded.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	listener, err := minecraft.ListenConfig{
		StatusProvider: minecraft.NewStatusProvider("BedrockForge", "BedrockForge"),
		ErrorLog:       logger,
	}.Listen("raknet", cfg.Listen)
	if err != nil {
		log.Fatalf("listen on %v: %v", cfg.Listen, err)
	}
	defer listener.Close()

	fmt.Printf("=== BedrockForge ===\n")
	fmt.Printf("Listening on %v, forwarding to %v\n", cfg.Listen, cfg.Server)
	fmt.Printf("Connect Minecraft to this device via LAN/Friends, or add a server pointing at it.\n")

	for {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		conn := c.(*minecraft.Conn)
		go handleConn(listener, conn, cfg.Server, src, mods, logger)
	}
}

func loadConfig() config {
	var cfg config
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("read config.json: %v", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config.json: %v", err)
	}
	return cfg
}
