// Command genmap compiles the Java -> Bedrock block state mapping asset used
// by the litematica mod.
//
// Inputs:
//   - Mojang's official block report (blocks.json), produced by the vanilla
//     data generator: java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports
//     It contains every Java block state with its explicit state ID.
//   - GeyserMC's blocks.nbt mapping (https://github.com/GeyserMC/mappings),
//     a list indexed by Java state ID with the Bedrock identifier/state.
//
// Output: a gzipped little-endian NBT file mapping every canonical Java state
// string to its Bedrock block state. Bare block names map to the block's
// default state as a fallback.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aodalt03-a11y/BedrockForge/proxy/mods/litematica"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type javaBlockReport map[string]struct {
	States []struct {
		ID         int               `json:"id"`
		Default    bool              `json:"default"`
		Properties map[string]string `json:"properties"`
	} `json:"states"`
}

func main() {
	report := flag.String("report", "blocks.json", "Mojang block report (from the data generator)")
	geyser := flag.String("geyser", "blocks.nbt", "Geyser blocks.nbt mapping")
	javaVersion := flag.String("java-version", "unknown", "Java version the inputs correspond to")
	out := flag.String("out", litematica.MappingFile, "output file")
	flag.Parse()

	reportData, err := os.ReadFile(*report)
	if err != nil {
		log.Fatal(err)
	}
	var blocks javaBlockReport
	if err := json.Unmarshal(reportData, &blocks); err != nil {
		log.Fatalf("parse %v: %v", *report, err)
	}

	gf, err := os.Open(*geyser)
	if err != nil {
		log.Fatal(err)
	}
	gz, err := gzip.NewReader(gf)
	if err != nil {
		log.Fatalf("gunzip %v: %v", *geyser, err)
	}
	var geyserRoot struct {
		Mappings []struct {
			BedrockIdentifier string         `nbt:"bedrock_identifier"`
			State             map[string]any `nbt:"state"`
		} `nbt:"bedrock_mappings"`
	}
	if err := nbt.NewDecoderWithEncoding(gz, nbt.BigEndian).Decode(&geyserRoot); err != nil {
		log.Fatalf("decode %v: %v", *geyser, err)
	}
	gf.Close()

	total := 0
	maxID := 0
	for _, b := range blocks {
		total += len(b.States)
		for _, s := range b.States {
			maxID = max(maxID, s.ID)
		}
	}
	if total != len(geyserRoot.Mappings) || maxID+1 != total {
		log.Fatalf("state count mismatch: report has %d states (max id %d), geyser has %d entries — inputs are for different versions",
			total, maxID, len(geyserRoot.Mappings))
	}

	entries := make(map[string]any, total+len(blocks))
	for javaName, b := range blocks {
		for _, st := range b.States {
			g := geyserRoot.Mappings[st.ID]
			bedrockName := g.BedrockIdentifier
			if bedrockName == "" {
				bedrockName = strings.TrimPrefix(javaName, "minecraft:")
			}
			entry := map[string]any{"n": "minecraft:" + bedrockName}
			if len(g.State) > 0 {
				entry["s"] = g.State
			}
			entries[litematica.CanonicalState(javaName, st.Properties)] = entry
			if st.Default {
				entries[javaName] = entry
			}
		}
	}

	of, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	gzw, _ := gzip.NewWriterLevel(of, gzip.BestCompression)
	root := map[string]any{
		"java_version": *javaVersion,
		"entries":      entries,
	}
	if err := nbt.NewEncoderWithEncoding(gzw, nbt.LittleEndian).Encode(root); err != nil {
		log.Fatalf("encode: %v", err)
	}
	if err := gzw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := of.Close(); err != nil {
		log.Fatal(err)
	}
	info, _ := os.Stat(*out)
	fmt.Printf("wrote %v: %d entries (%d java states + %d defaults), %d bytes\n",
		*out, len(entries), total, len(entries)-total, info.Size())
}
