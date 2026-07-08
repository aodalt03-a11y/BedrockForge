// Package core defines the mod interface and per-player session shared by the
// proxy and its mods.
package core

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Mod is a proxy mod. Packet hooks run on every packet in the direction named;
// returning drop=true stops the packet from being forwarded.
type Mod interface {
	Name() string
	// OnJoin is called once both sides of a session have spawned.
	OnJoin(s *Session)
	// OnClientPacket handles packets sent by the game client.
	OnClientPacket(s *Session, pk packet.Packet) (drop bool)
	// OnServerPacket handles packets sent by the remote server.
	OnServerPacket(s *Session, pk packet.Packet) (drop bool)
	// Commands returns the chat commands the mod responds to.
	Commands() []Command
}

// Command is a chat command, invoked by the player as ".name args...".
type Command struct {
	Name  string
	Usage string
	Run   func(s *Session, args []string)
}

// Session is one proxied player connection. It is the context passed to all
// mod hooks and provides packet injection plus a few convenience helpers.
type Session struct {
	client *minecraft.Conn
	server *minecraft.Conn
	mods   []Mod

	mu   sync.Mutex
	data map[string]any // per-session mod storage

	posMu sync.Mutex
	posX  float32
	posY  float32
	posZ  float32
}

// NewSession wraps a spawned client/server connection pair.
func NewSession(client, server *minecraft.Conn, mods []Mod) *Session {
	return &Session{client: client, server: server, mods: mods, data: map[string]any{}}
}

// SendToClient injects a packet toward the game client.
func (s *Session) SendToClient(pk packet.Packet) error { return s.client.WritePacket(pk) }

// SendToServer injects a packet toward the remote server.
func (s *Session) SendToServer(pk packet.Packet) error { return s.server.WritePacket(pk) }

// GameData returns the game data the remote server started the session with.
func (s *Session) GameData() minecraft.GameData { return s.server.GameData() }

// PlayerName returns the display name of the connected player.
func (s *Session) PlayerName() string { return s.client.IdentityData().DisplayName }

// Message sends a raw chat message to the player, prefixed with the
// BedrockForge tag.
func (s *Session) Message(format string, a ...any) {
	_ = s.SendToClient(&packet.Text{
		TextType: packet.TextTypeRaw,
		Message:  "§8[§2BF§8]§r " + fmt.Sprintf(format, a...),
	})
}

// SetPlayerPos records the player's current feet position.
func (s *Session) SetPlayerPos(x, y, z float32) {
	s.posMu.Lock()
	s.posX, s.posY, s.posZ = x, y, z
	s.posMu.Unlock()
}

// PlayerPos returns the player's last reported feet position.
func (s *Session) PlayerPos() (x, y, z float32) {
	s.posMu.Lock()
	defer s.posMu.Unlock()
	return s.posX, s.posY, s.posZ
}

// Store saves a per-session value for a mod; Load retrieves it.
func (s *Session) Store(key string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = v
}

func (s *Session) Load(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

// DispatchCommand routes a ".cmd args" chat message to the owning mod.
// It returns true if the message was consumed as a command.
func (s *Session) DispatchCommand(msg string) bool {
	if !strings.HasPrefix(msg, ".") {
		return false
	}
	fields := strings.Fields(msg[1:])
	if len(fields) == 0 {
		return false
	}
	name, args := strings.ToLower(fields[0]), fields[1:]

	if name == "help" {
		s.Message("Commands:")
		s.Message(".help - show this help")
		for _, m := range s.mods {
			for _, c := range m.Commands() {
				s.Message(".%v %v", c.Name, c.Usage)
			}
		}
		return true
	}
	for _, m := range s.mods {
		for _, c := range m.Commands() {
			if c.Name == name {
				c.Run(s, args)
				return true
			}
		}
	}
	s.Message("Unknown command .%v — try .help", name)
	return true
}
