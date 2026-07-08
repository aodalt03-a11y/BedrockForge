package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aodalt03-a11y/BedrockForge/proxy/core"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/oauth2"
)

// handleConn proxies a single accepted client connection to the remote server.
func handleConn(listener *minecraft.Listener, client *minecraft.Conn, remote string, src oauth2.TokenSource, mods []core.Mod) {
	name := client.IdentityData().DisplayName
	log.Printf("[%v] connected, dialing %v", name, remote)

	server, err := minecraft.Dialer{
		TokenSource: src,
		ClientData:  client.ClientData(),
	}.DialTimeout("raknet", remote, 30*time.Second)
	if err != nil {
		log.Printf("[%v] dial %v: %v", name, remote, err)
		_ = listener.Disconnect(client, fmt.Sprintf("BedrockForge: could not reach %v", remote))
		return
	}
	persistToken(src)

	var g sync.WaitGroup
	g.Add(2)
	var startErr, spawnErr error
	go func() {
		startErr = client.StartGame(server.GameData())
		g.Done()
	}()
	go func() {
		spawnErr = server.DoSpawn()
		g.Done()
	}()
	g.Wait()
	if startErr != nil || spawnErr != nil {
		log.Printf("[%v] spawn failed: %v %v", name, startErr, spawnErr)
		_ = listener.Disconnect(client, "BedrockForge: spawn failed")
		_ = server.Close()
		return
	}
	log.Printf("[%v] spawned", name)

	s := core.NewSession(client, server, mods)
	for _, m := range mods {
		m.OnJoin(s)
	}

	// Client -> server.
	go func() {
		defer server.Close()
		defer listener.Disconnect(client, "connection lost")
		for {
			pk, err := client.ReadPacket()
			if err != nil {
				return
			}

			switch p := pk.(type) {
			case *packet.PlayerAuthInput:
				// Position is at eye height; subtract the 1.62 offset for feet.
				s.SetPlayerPos(p.Position.X(), p.Position.Y()-1.62, p.Position.Z())
			case *packet.Text:
				if p.TextType == packet.TextTypeChat && s.DispatchCommand(p.Message) {
					continue
				}
			}

			drop := false
			for _, m := range mods {
				if m.OnClientPacket(s, pk) {
					drop = true
				}
			}
			if drop {
				continue
			}
			if err := server.WritePacket(pk); err != nil {
				return
			}
		}
	}()

	// Server -> client.
	go func() {
		defer server.Close()
		for {
			pk, err := server.ReadPacket()
			if err != nil {
				var disc minecraft.DisconnectError
				if errors.As(err, &disc) {
					_ = listener.Disconnect(client, disc.Error())
				} else {
					_ = listener.Disconnect(client, "server connection lost")
				}
				return
			}

			drop := false
			for _, m := range mods {
				if m.OnServerPacket(s, pk) {
					drop = true
				}
			}
			if drop {
				continue
			}
			if err := client.WritePacket(pk); err != nil {
				return
			}
		}
	}()
}
