package litematica

import (
	"sync"
	"time"

	"github.com/aodalt03-a11y/BedrockForge/proxy/core"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// state is the per-session litematica state.
type state struct {
	mu        sync.Mutex
	schem     *Schematic
	origin    [3]int32
	originSet bool
	placed    []protocol.BlockPos
	placing   bool
}

// batch tuning: how many UpdateBlock packets to send before a short pause, so
// huge schematics don't choke the client connection.
const (
	batchSize  = 1000
	batchPause = 20 * time.Millisecond
)

func (m *Mod) cmdPlace(s *core.Session, _ []string) {
	if m.mapErr != nil {
		s.Message("§cBlock mappings unavailable: %v", m.mapErr)
		return
	}
	if !s.GameData().UseBlockNetworkIDHashes {
		s.Message("§cThis server uses legacy block IDs; ghost blocks are not supported on it.")
		return
	}

	st := m.state(s)
	st.mu.Lock()
	if st.schem == nil {
		st.mu.Unlock()
		s.Message("No schematic loaded. Use .load <name>")
		return
	}
	if st.placing {
		st.mu.Unlock()
		s.Message("Already placing, hold on...")
		return
	}
	if !st.originSet {
		fx, fy, fz := s.PlayerPos()
		st.origin = [3]int32{floor32(fx), floor32(fy + 0.5), floor32(fz)}
		st.originSet = true
		s.Message("Origin set to your position: %d %d %d", st.origin[0], st.origin[1], st.origin[2])
	}
	st.placing = true
	schem, origin, oldPlaced := st.schem, st.origin, st.placed
	st.placed = nil
	st.mu.Unlock()

	go func() {
		defer func() {
			st.mu.Lock()
			st.placing = false
			st.mu.Unlock()
		}()

		// Clear any previous render first so .place can be used to move a
		// schematic without leaving stale ghosts behind.
		if len(oldPlaced) > 0 {
			m.sendBlocks(s, oldPlaced, m.mapper.AirRID())
		}

		// Resolve each palette entry once.
		rids := make([]uint32, len(schem.Palette))
		mapped := make([]bool, len(schem.Palette))
		unmappedStates := 0
		for i, js := range schem.Palette {
			bs, ok := m.mapper.Lookup(js.Name, js.Props)
			if !ok {
				unmappedStates++
				continue
			}
			rids[i] = HashBlock(bs.Name, bs.States)
			mapped[i] = true
		}

		placed := make([]protocol.BlockPos, 0, len(schem.Blocks))
		skipped, sent := 0, 0
		for _, b := range schem.Blocks {
			if !mapped[b.Palette] {
				skipped++
				continue
			}
			pos := protocol.BlockPos{origin[0] + b.X, origin[1] + b.Y, origin[2] + b.Z}
			_ = s.SendToClient(&packet.UpdateBlock{
				Position:          pos,
				NewBlockRuntimeID: rids[b.Palette],
				Flags:             packet.BlockUpdateNetwork,
				Layer:             0,
			})
			placed = append(placed, pos)
			sent++
			if sent%batchSize == 0 {
				time.Sleep(batchPause)
			}
			if sent%10000 == 0 {
				s.Message("Placing... %d/%d", sent, len(schem.Blocks))
			}
		}

		st.mu.Lock()
		st.placed = placed
		st.mu.Unlock()

		s.Message("Placed §a%d§r ghost blocks at %d %d %d", sent, origin[0], origin[1], origin[2])
		if skipped > 0 {
			s.Message("§7%d blocks had no Bedrock equivalent (%d palette states) and were skipped.", skipped, unmappedStates)
		}
		s.Message("§7Ghosts only show in loaded chunks; they vanish if chunks reload — use .place again.")
	}()
}

func (m *Mod) cmdClear(s *core.Session, _ []string) {
	st := m.state(s)
	st.mu.Lock()
	placed := st.placed
	st.placed = nil
	st.mu.Unlock()
	if len(placed) == 0 {
		s.Message("Nothing to clear.")
		return
	}
	go func() {
		m.sendBlocks(s, placed, m.mapper.AirRID())
		s.Message("Cleared %d ghost blocks. If real blocks were hidden, rejoin to refresh.", len(placed))
	}()
}

func (m *Mod) sendBlocks(s *core.Session, positions []protocol.BlockPos, rid uint32) {
	for i, pos := range positions {
		_ = s.SendToClient(&packet.UpdateBlock{
			Position:          pos,
			NewBlockRuntimeID: rid,
			Flags:             packet.BlockUpdateNetwork,
			Layer:             0,
		})
		if (i+1)%batchSize == 0 {
			time.Sleep(batchPause)
		}
	}
}
