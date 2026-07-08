package litematica

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// packBits packs palette indices into int64s the way Litematica does:
// tightly packed, entries spanning long boundaries.
func packBits(indices []uint64, bitsPer int) []int64 {
	longs := make([]int64, (len(indices)*bitsPer+63)/64)
	for i, v := range indices {
		bitOff := i * bitsPer
		long, bit := bitOff>>6, bitOff&63
		longs[long] |= int64(v << bit)
		if bit+bitsPer > 64 {
			longs[long+1] |= int64(v >> (64 - bit))
		}
	}
	return longs
}

func writeLitematic(t *testing.T, path string, root map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := nbt.NewEncoderWithEncoding(gz, nbt.BigEndian).Encode(root); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseLitematic(t *testing.T) {
	// 2x2x2 region: air except two known blocks.
	// Palette: 0=air, 1=stone, 2=oak_stairs[facing=east].
	// Index order is x fastest, then z, then y.
	indices := make([]uint64, 8)
	indices[0] = 1 // (0,0,0) stone
	indices[7] = 2 // (1,1,1) stairs
	longs := packBits(indices, 2)

	root := map[string]any{
		"Version":              int32(6),
		"MinecraftDataVersion": int32(4000),
		"Metadata":             map[string]any{"Name": "test"},
		"Regions": map[string]any{
			"main": map[string]any{
				"Position": map[string]any{"x": int32(10), "y": int32(-2), "z": int32(5)},
				"Size":     map[string]any{"x": int32(2), "y": int32(2), "z": int32(2)},
				"BlockStatePalette": []any{
					map[string]any{"Name": "minecraft:air"},
					map[string]any{"Name": "minecraft:stone"},
					map[string]any{
						"Name":       "minecraft:oak_stairs",
						"Properties": map[string]any{"facing": "east", "half": "bottom"},
					},
				},
				"BlockStates": longs,
			},
		},
	}

	path := filepath.Join(t.TempDir(), "test.litematic")
	writeLitematic(t, path, root)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Blocks) != 2 {
		t.Fatalf("expected 2 non-air blocks, got %d", len(s.Blocks))
	}
	stone, stairs := s.Blocks[0], s.Blocks[1]
	if stone.X != 10 || stone.Y != -2 || stone.Z != 5 {
		t.Errorf("stone at %d,%d,%d; want 10,-2,5", stone.X, stone.Y, stone.Z)
	}
	if s.Palette[stone.Palette].Name != "minecraft:stone" {
		t.Errorf("stone palette entry: %+v", s.Palette[stone.Palette])
	}
	if stairs.X != 11 || stairs.Y != -1 || stairs.Z != 6 {
		t.Errorf("stairs at %d,%d,%d; want 11,-1,6", stairs.X, stairs.Y, stairs.Z)
	}
	if p := s.Palette[stairs.Palette]; p.Name != "minecraft:oak_stairs" || p.Props["facing"] != "east" {
		t.Errorf("stairs palette entry: %+v", p)
	}
}

func TestParseLitematicNegativeSize(t *testing.T) {
	// Litematica encodes some regions with negative sizes extending backwards
	// from Position.
	indices := []uint64{1} // single stone block
	root := map[string]any{
		"Regions": map[string]any{
			"r": map[string]any{
				"Position": map[string]any{"x": int32(0), "y": int32(0), "z": int32(0)},
				"Size":     map[string]any{"x": int32(-1), "y": int32(1), "z": int32(-1)},
				"BlockStatePalette": []any{
					map[string]any{"Name": "minecraft:air"},
					map[string]any{"Name": "minecraft:stone"},
				},
				"BlockStates": packBits(indices, 2),
			},
		},
	}
	path := filepath.Join(t.TempDir(), "neg.litematic")
	writeLitematic(t, path, root)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(s.Blocks))
	}
	if b := s.Blocks[0]; b.X != 0 || b.Y != 0 || b.Z != 0 {
		t.Errorf("block at %d,%d,%d; want 0,0,0", b.X, b.Y, b.Z)
	}
}

func TestCanonicalState(t *testing.T) {
	got := CanonicalState("minecraft:oak_stairs", map[string]string{
		"half": "bottom", "facing": "east",
	})
	want := "minecraft:oak_stairs[facing=east,half=bottom]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if CanonicalState("stone", nil) != "minecraft:stone" {
		t.Errorf("bare name canonicalization failed")
	}
}

func TestHashBlockDistinct(t *testing.T) {
	// The hash must special-case minecraft:unknown and produce distinct
	// values for different states of a block.
	if HashBlock("minecraft:unknown", nil) != 0xfffffffe {
		t.Error("minecraft:unknown must hash to -2")
	}
	a := HashBlock("minecraft:oak_stairs", map[string]any{"weirdo_direction": int32(0), "upside_down_bit": uint8(0)})
	b := HashBlock("minecraft:oak_stairs", map[string]any{"weirdo_direction": int32(1), "upside_down_bit": uint8(0)})
	if a == b {
		t.Error("different states must hash differently")
	}
}
