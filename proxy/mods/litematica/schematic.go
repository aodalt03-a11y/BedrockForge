package litematica

import (
	"compress/gzip"
	"fmt"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// JavaState is a Java edition block state from a schematic palette.
type JavaState struct {
	Name  string
	Props map[string]string
}

// Block is a single non-air block: coordinates relative to the schematic
// origin plus an index into the palette.
type Block struct {
	X, Y, Z int32
	Palette int32
}

// Schematic is a parsed, air-stripped schematic.
type Schematic struct {
	Name    string
	Palette []JavaState
	Blocks  []Block
	Min     [3]int32
	Max     [3]int32
}

func isAir(name string) bool {
	switch strings.TrimPrefix(name, "minecraft:") {
	case "air", "cave_air", "void_air":
		return true
	}
	return false
}

// ParseFile parses a schematic file based on its extension. Supported are
// .litematic (Litematica) and .schem (Sponge v2/v3).
func ParseFile(path string) (*Schematic, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".litematic"):
		return parseLitematic(base, data)
	case strings.HasSuffix(base, ".schem"):
		return parseSchem(base, data)
	default:
		return nil, fmt.Errorf("unsupported file type: %v (use .litematic or .schem)", base)
	}
}

func decodeGzipNBT(data []byte) (map[string]any, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("not gzip-compressed: %w", err)
	}
	var m map[string]any
	if err := nbt.NewDecoderWithEncoding(gz, nbt.BigEndian).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode nbt: %w", err)
	}
	return m, nil
}

func (s *Schematic) grow(x, y, z int32) {
	s.Min[0], s.Max[0] = min(s.Min[0], x), max(s.Max[0], x)
	s.Min[1], s.Max[1] = min(s.Min[1], y), max(s.Max[1], y)
	s.Min[2], s.Max[2] = min(s.Min[2], z), max(s.Max[2], z)
}

// --- Litematica format ---

func parseLitematic(name string, data []byte) (*Schematic, error) {
	root, err := decodeGzipNBT(data)
	if err != nil {
		return nil, err
	}
	regions, ok := root["Regions"].(map[string]any)
	if !ok || len(regions) == 0 {
		return nil, fmt.Errorf("no regions found; not a litematic?")
	}

	s := &Schematic{
		Name: strings.TrimSuffix(name, ".litematic"),
		Min:  [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32},
		Max:  [3]int32{math.MinInt32, math.MinInt32, math.MinInt32},
	}

	for regionName, rv := range regions {
		region, ok := rv.(map[string]any)
		if !ok {
			continue
		}
		if err := s.parseRegion(region); err != nil {
			return nil, fmt.Errorf("region %q: %w", regionName, err)
		}
	}
	if len(s.Blocks) == 0 {
		s.Min, s.Max = [3]int32{}, [3]int32{}
	}
	return s, nil
}

func vec3i(v any) (x, y, z int32, err error) {
	m, ok := v.(map[string]any)
	if !ok {
		return 0, 0, 0, fmt.Errorf("expected compound vector, got %T", v)
	}
	xi, _ := m["x"].(int32)
	yi, _ := m["y"].(int32)
	zi, _ := m["z"].(int32)
	return xi, yi, zi, nil
}

func (s *Schematic) parseRegion(region map[string]any) error {
	posX, posY, posZ, err := vec3i(region["Position"])
	if err != nil {
		return fmt.Errorf("Position: %w", err)
	}
	sizeX, sizeY, sizeZ, err := vec3i(region["Size"])
	if err != nil {
		return fmt.Errorf("Size: %w", err)
	}

	// Negative sizes mean the region extends in the negative direction from
	// Position. Normalize to a min corner and absolute dimensions.
	norm := func(pos, size int32) (int32, int32) {
		if size < 0 {
			return pos + size + 1, -size
		}
		return pos, size
	}
	minX, dimX := norm(posX, sizeX)
	minY, dimY := norm(posY, sizeY)
	minZ, dimZ := norm(posZ, sizeZ)
	volume := int(dimX) * int(dimY) * int(dimZ)
	if volume == 0 {
		return nil
	}

	paletteList, ok := region["BlockStatePalette"].([]any)
	if !ok {
		return fmt.Errorf("missing BlockStatePalette")
	}
	paletteBase := int32(len(s.Palette))
	airLocal := make([]bool, len(paletteList))
	for i, pv := range paletteList {
		pm, ok := pv.(map[string]any)
		if !ok {
			return fmt.Errorf("palette entry %d: expected compound", i)
		}
		name, _ := pm["Name"].(string)
		props := map[string]string{}
		if pp, ok := pm["Properties"].(map[string]any); ok {
			for k, v := range pp {
				if sv, ok := v.(string); ok {
					props[k] = sv
				}
			}
		}
		airLocal[i] = isAir(name)
		s.Palette = append(s.Palette, JavaState{Name: name, Props: props})
	}

	longs, ok := region["BlockStates"].([]int64)
	if !ok {
		return fmt.Errorf("missing BlockStates")
	}
	bitsPer := max(2, bits.Len(uint(len(paletteList)-1)))
	if need := (volume*bitsPer + 63) / 64; len(longs) < need {
		return fmt.Errorf("BlockStates too short: have %d longs, need %d", len(longs), need)
	}

	// Blocks are stored tightly bit-packed (entries span across longs),
	// index order x fastest, then z, then y.
	mask := uint64(1)<<bitsPer - 1
	idx := 0
	for y := int32(0); y < dimY; y++ {
		for z := int32(0); z < dimZ; z++ {
			for x := int32(0); x < dimX; x++ {
				bitOff := idx * bitsPer
				long := bitOff >> 6
				bit := bitOff & 63
				v := uint64(longs[long]) >> bit
				if bit+bitsPer > 64 {
					v |= uint64(longs[long+1]) << (64 - bit)
				}
				pi := int32(v & mask)
				idx++
				if int(pi) >= len(paletteList) || airLocal[pi] {
					continue
				}
				bx, by, bz := minX+x, minY+y, minZ+z
				s.Blocks = append(s.Blocks, Block{X: bx, Y: by, Z: bz, Palette: paletteBase + pi})
				s.grow(bx, by, bz)
			}
		}
	}
	return nil
}

// --- Sponge .schem format (v2/v3) ---

func parseSchem(name string, data []byte) (*Schematic, error) {
	root, err := decodeGzipNBT(data)
	if err != nil {
		return nil, err
	}
	// v3 wraps everything in a "Schematic" compound.
	if inner, ok := root["Schematic"].(map[string]any); ok {
		root = inner
	}

	width, _ := root["Width"].(int16)
	height, _ := root["Height"].(int16)
	length, _ := root["Length"].(int16)
	if width <= 0 || height <= 0 || length <= 0 {
		return nil, fmt.Errorf("invalid dimensions %dx%dx%d", width, height, length)
	}

	paletteNBT, blockData := root["Palette"], root["BlockData"]
	if blocks, ok := root["Blocks"].(map[string]any); ok { // v3
		paletteNBT, blockData = blocks["Palette"], blocks["Data"]
	}
	paletteMap, ok := paletteNBT.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing palette")
	}
	dataBytes, ok := blockData.([]byte)
	if !ok {
		return nil, fmt.Errorf("missing block data")
	}

	s := &Schematic{
		Name: strings.TrimSuffix(name, ".schem"),
		Min:  [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32},
		Max:  [3]int32{math.MinInt32, math.MinInt32, math.MinInt32},
	}

	maxID := int32(-1)
	ids := map[int32]int32{} // schem palette id -> s.Palette index
	airIDs := map[int32]bool{}
	for stateStr, idv := range paletteMap {
		id, ok := idv.(int32)
		if !ok {
			continue
		}
		st := parseStateString(stateStr)
		if isAir(st.Name) {
			airIDs[id] = true
			continue
		}
		ids[id] = int32(len(s.Palette))
		s.Palette = append(s.Palette, st)
		maxID = max(maxID, id)
	}

	w, l := int32(width), int32(length)
	i, idx := 0, int32(0)
	for i < len(dataBytes) {
		// Block data is a varint stream of palette IDs.
		var v, shift uint32
		for {
			b := dataBytes[i]
			i++
			v |= uint32(b&0x7f) << shift
			if b&0x80 == 0 {
				break
			}
			shift += 7
		}
		id := int32(v)
		x := idx % w
		z := (idx / w) % l
		y := idx / (w * l)
		idx++
		if airIDs[id] {
			continue
		}
		pi, ok := ids[id]
		if !ok {
			continue
		}
		s.Blocks = append(s.Blocks, Block{X: x, Y: y, Z: z, Palette: pi})
		s.grow(x, y, z)
	}
	if len(s.Blocks) == 0 {
		s.Min, s.Max = [3]int32{}, [3]int32{}
	}
	return s, nil
}

// parseStateString parses "minecraft:oak_stairs[facing=east,half=bottom]".
func parseStateString(str string) JavaState {
	name, propsStr, found := strings.Cut(str, "[")
	st := JavaState{Name: name, Props: map[string]string{}}
	if !found {
		return st
	}
	propsStr = strings.TrimSuffix(propsStr, "]")
	for _, kv := range strings.Split(propsStr, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			st.Props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return st
}
