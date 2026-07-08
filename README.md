# BedrockForge

A proxy-based mod launcher for **Minecraft: Bedrock Edition** on Android.

BedrockForge runs a man-in-the-middle proxy on your phone. You point Minecraft
at the proxy and the proxy connects to the real server, letting mods inspect
and inject packets. The first bundled mod is **litematica**: it renders Java
edition schematics (`.litematic`, `.schem`) as client-side ghost blocks so you
can copy builds block-by-block, like the Litematica mod on Java.

## How to use

1. Install the APK (built by GitHub Actions, see below).
2. Open BedrockForge, tap **Login** and sign in with your Xbox/Microsoft
   account (the account you play Minecraft with).
3. Import a `.litematic` or `.schem` file with **Add Schematic**.
4. Enter the server address (e.g. `play.example.com:19132`) and tap
   **START PROXY**.
5. In Minecraft, add a server with address `127.0.0.1` port `19132`
   (same phone) and join it.
6. In game chat, type:
   - `.help` — list commands
   - `.list` — show imported schematics
   - `.load <name>` — load one
   - `.place` — render ghost blocks where you stand (or `.origin x y z` first)
   - `.clear` — remove the ghosts

Ghost blocks are client-side only — nothing is sent to the server, and they
disappear when chunks reload (just run `.place` again).

## Layout

- `proxy/` — the Go proxy (gophertunnel). Mods live in `proxy/mods/`,
  the mod/session API in `proxy/core/`.
- `app/` — the Android launcher app (Kotlin). It runs the proxy binary, which
  is packaged as `libmcproxy.so` so Android allows executing it.
- `proxy/cmd/genmap` — generator for the Java→Bedrock block mapping asset
  (`app/src/main/assets/java_bedrock_map.nbt.gz`).

## Block mapping

Ghost blocks use Bedrock's *hashed block network IDs* (FNV-1a over the block
state NBT), which are stable across game versions — no per-version runtime ID
tables. Java→Bedrock state mapping is compiled from Mojang's official block
report plus [GeyserMC mappings](https://github.com/GeyserMC/mappings).

To regenerate after a Minecraft update:

```sh
# 1. Mojang block report (needs the matching server.jar + recent JRE):
java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports
# 2. Geyser mappings:
curl -LO https://raw.githubusercontent.com/GeyserMC/mappings/master/blocks.nbt
# 3. Compile the asset:
cd proxy && go run ./cmd/genmap \
  -report generated/reports/blocks.json -geyser blocks.nbt \
  -java-version <version> -out ../app/src/main/assets/java_bedrock_map.nbt.gz
```

## Building

CI builds everything: the Go proxy is cross-compiled for Android arm64 with
the NDK, packed into the APK as a native lib, and the signed APK is uploaded
as a workflow artifact. See `.github/workflows/build.yml`.

Local proxy development (any OS):

```sh
cd proxy
go test ./...
go build .   # then run with a config.json next to the binary
```
