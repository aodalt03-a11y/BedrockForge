// Package litematica is a BedrockForge mod that renders Java edition
// schematics (.litematic, .schem) as client-side ghost blocks, similar to the
// Litematica mod for Java edition.
package litematica

import (
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/aodalt03-a11y/BedrockForge/proxy/core"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const schemDir = "schematics"

// Mod implements core.Mod.
type Mod struct {
	mapper *BlockMapper
	mapErr error
}

// New loads the block mapping table and returns the mod.
func New() *Mod {
	m := &Mod{}
	m.mapper, m.mapErr = LoadMapper()
	if m.mapErr != nil {
		log.Printf("litematica: block mappings unavailable: %v", m.mapErr)
	} else {
		log.Printf("litematica: loaded %d block state mappings", m.mapper.Len())
	}
	return m
}

func (m *Mod) Name() string { return "litematica" }

func (m *Mod) OnJoin(s *core.Session) {
	s.Store("lm", &state{})
	s.Message("BedrockForge active. Type §a.help§r in chat.")
	if m.mapErr != nil {
		s.Message("§cWarning: block mappings failed to load, ghost blocks disabled.")
	} else if !s.GameData().UseBlockNetworkIDHashes {
		s.Message("§cWarning: this server uses legacy block IDs; ghost blocks are not supported on it.")
	}
}

func (m *Mod) OnClientPacket(*core.Session, packet.Packet) bool { return false }

func (m *Mod) OnServerPacket(s *core.Session, pk packet.Packet) bool {
	switch pk.(type) {
	case *packet.ChangeDimension:
		if st := m.state(s); st != nil {
			st.mu.Lock()
			cleared := len(st.placed) > 0
			st.placed = nil
			st.mu.Unlock()
			if cleared {
				s.Message("Dimension changed, ghost blocks reset. Use .place to re-render.")
			}
		}
	}
	return false
}

func (m *Mod) state(s *core.Session) *state {
	st, _ := s.Load("lm").(*state)
	return st
}

func (m *Mod) Commands() []core.Command {
	return []core.Command{
		{Name: "list", Usage: "- list imported schematics", Run: m.cmdList},
		{Name: "load", Usage: "<name> - load a schematic", Run: m.cmdLoad},
		{Name: "origin", Usage: "[x y z] - set build origin (default: where you stand)", Run: m.cmdOrigin},
		{Name: "place", Usage: "- render ghost blocks at the origin", Run: m.cmdPlace},
		{Name: "clear", Usage: "- remove ghost blocks", Run: m.cmdClear},
		{Name: "info", Usage: "- show loaded schematic info", Run: m.cmdInfo},
	}
}

func (m *Mod) cmdList(s *core.Session, _ []string) {
	entries, err := os.ReadDir(schemDir)
	if err != nil {
		s.Message("§cNo schematics folder found.")
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		s.Message("No schematics imported yet. Add them in the BedrockForge app.")
		return
	}
	s.Message("Schematics (%d):", len(names))
	for _, n := range names {
		s.Message("§7- %v", n)
	}
}

func (m *Mod) cmdLoad(s *core.Session, args []string) {
	if len(args) == 0 {
		s.Message("Usage: .load <name>")
		return
	}
	query := strings.ToLower(strings.Join(args, " "))
	entries, err := os.ReadDir(schemDir)
	if err != nil {
		s.Message("§cNo schematics folder found.")
		return
	}
	var match string
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if n == query || strings.TrimSuffix(strings.TrimSuffix(n, ".litematic"), ".schem") == query {
			match = e.Name()
			break
		}
		if strings.Contains(n, query) && match == "" {
			match = e.Name()
		}
	}
	if match == "" {
		s.Message("§cNo schematic matching %q. Try .list", query)
		return
	}

	schem, err := ParseFile(schemDir + "/" + match)
	if err != nil {
		s.Message("§cFailed to parse %v: %v", match, err)
		return
	}
	st := m.state(s)
	st.mu.Lock()
	st.schem = schem
	st.originSet = false
	st.mu.Unlock()

	sz := [3]int32{schem.Max[0] - schem.Min[0] + 1, schem.Max[1] - schem.Min[1] + 1, schem.Max[2] - schem.Min[2] + 1}
	s.Message("Loaded §a%v§r: %d blocks, %dx%dx%d", schem.Name, len(schem.Blocks), sz[0], sz[1], sz[2])
	s.Message("Stand at the build spot and type §a.place§r (or set §a.origin§r first).")
}

func (m *Mod) cmdOrigin(s *core.Session, args []string) {
	st := m.state(s)
	var x, y, z int32
	if len(args) >= 3 {
		xi, err1 := strconv.Atoi(args[0])
		yi, err2 := strconv.Atoi(args[1])
		zi, err3 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil || err3 != nil {
			s.Message("Usage: .origin [x y z]")
			return
		}
		x, y, z = int32(xi), int32(yi), int32(zi)
	} else {
		fx, fy, fz := s.PlayerPos()
		x, y, z = floor32(fx), floor32(fy+0.5), floor32(fz)
	}
	st.mu.Lock()
	st.origin = [3]int32{x, y, z}
	st.originSet = true
	st.mu.Unlock()
	s.Message("Origin set to %d %d %d", x, y, z)
}

func (m *Mod) cmdInfo(s *core.Session, _ []string) {
	st := m.state(s)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.schem == nil {
		s.Message("No schematic loaded. Use .load <name>")
		return
	}
	sc := st.schem
	s.Message("Schematic: §a%v§r, %d blocks", sc.Name, len(sc.Blocks))
	s.Message("Bounds: %v..%v", sc.Min, sc.Max)
	if st.originSet {
		s.Message("Origin: %d %d %d (%d ghosts placed)", st.origin[0], st.origin[1], st.origin[2], len(st.placed))
	} else {
		s.Message("Origin: not set (will use your position)")
	}
}

func floor32(f float32) int32 {
	i := int32(f)
	if f < 0 && float32(i) != f {
		i--
	}
	return i
}
