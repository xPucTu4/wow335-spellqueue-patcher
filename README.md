# WoW 3.3.5a Spell Queue Patcher

This patch lets a WoW 3.3.5a build 12340 client send a spell request while a cast or global cooldown is still active. A compatible private server can then queue a request made near the end of the current cast instead of the client rejecting the keypress locally.

The patch does **not** remove the global cooldown, shorten casts, or make the client decide which casts are legal. AzerothCore remains authoritative. A request made inside `SpellQueue.Window` is queued; one made too early is rejected by the server.

Both local cooldown bypasses are unconditional, not limited to the end of a cast or GCD. This also applies to long spell cooldowns: repeated attempts that pass the remaining client checks send additional cast requests, even when the spell cannot be used yet. Rejections for those requests now depend on a server round trip instead of an immediate local cooldown error. The patch does not generate requests on its own; extra traffic depends on how often you attempt to cast.

## Before you begin

- Use a Windows WoW 3.3.5a client reporting build 12340.
- Back up `Wow.exe`. The patcher creates a new file and never overwrites its input.
- Your server must support and enable spell queueing. For AzerothCore:

  ```ini
  SpellQueue.Enabled = 1
  SpellQueue.Window = 400
  ```

  This project was tested with a 1000 ms window to make diagnosis obvious. A smaller value such as 400 ms is closer to the intended near-end queue behavior.

  With `SpellQueue.Enabled = 0`, the patched client still sends these requests, but the server does not queue them. Unavailable casts are rejected normally, so you retain the extra traffic and server-dependent feedback without the queueing benefit.

No Blizzard client files are included in this project.

## Windows

Download `wow335-spellqueue-patcher.exe` from the [latest release](https://github.com/xPucTu4/wow335-spellqueue-patcher/releases/latest). The supplied Windows binary is amd64 (64-bit); the client executable it patches is still PE32/i386.

Put `wow335-spellqueue-patcher.exe` beside `Wow.exe`, open a terminal in that directory, and run:

```powershell
.\wow335-spellqueue-patcher.exe --check .\Wow.exe
.\wow335-spellqueue-patcher.exe --output .\Wow-spellqueue.exe .\Wow.exe
```

Keep the original `Wow.exe` as your backup. To use the patched client, either launch `Wow-spellqueue.exe` directly or rename the files so the patched copy is named `Wow.exe`.

The patcher is an unsigned community executable, so Windows may show a reputation warning. Build it from the short Go source if you do not want to run the supplied binary.

## Linux

The Linux patcher modifies the Windows client before it is launched through Wine or Proton.

Download the Linux amd64 executable `wow335-spellqueue-patcher` from the [latest release](https://github.com/xPucTu4/wow335-spellqueue-patcher/releases/latest).

```bash
chmod +x wow335-spellqueue-patcher
./wow335-spellqueue-patcher --check /path/to/Wow.exe
./wow335-spellqueue-patcher --output /path/to/Wow-spellqueue.exe /path/to/Wow.exe
```

## What success looks like

1. Start a cast-time spell such as Fireball.
2. Press it again near the end of the cast.
3. The next cast begins immediately after the first finishes.
4. Pressing too early may still produce a server rejection. That is expected and proves the server, not the modified client, controls the queue window.

The developer manually tested queueing with many spells against AzerothCore. Fireball is the detailed packet-trace example documented in [TECHNICAL.md](TECHNICAL.md#runtime-evidence): the patched client transmitted the early request, AzerothCore queued it, and the next cast began without interrupting the active cast. That trace is not the full extent of manual testing, nor is it an exhaustive compatibility matrix for every spell, macro, pet, or item-use path.

## Compatibility and safety

The patcher accepts PE32/i386 WoW 3.3.5a build 12340 clients only when both surrounding code signatures occur exactly once in `.text` and their branch bytes are recognized. It was verified against:

- the pristine client used during development;
- one composite client with RCE-prevention, large-address-aware, camera, and other static patches;
- partially patched and already patched copies.

It deliberately refuses an unknown, ambiguous, or structurally incompatible executable. A static offset alone would be unsafe because another client modification can move or replace code. The signatures identify the instructions by their surrounding machine code instead.

Other modifications are compatible only if they preserve those code sites. The patcher does not discover relocated code outside `.text` or audit other client patches, and matching signatures do not certify an executable as safe.

Already-patched input exits successfully and explicitly reports that no output was written. An existing output is never overwritten: choose another `--output`, or remove the old output yourself if you no longer need it. To undo the patch, restore your original backup.

Exit status is `0` for successful patching, compatible `--check`, already-patched input, `--help`, and `--version`; `1` means an error, including incompatible input or a refused output. Put flags before the input filename.

This is intended for compatible private servers. It does not make the 3.3.5a client compatible with modern official servers. See [TECHNICAL.md](TECHNICAL.md) for hashes, exact hex edits, reverse-engineering notes, failed approaches, and reproduction instructions.

## Building from source

Install Go 1.22 or newer, then run:

```bash
go test ./...
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o wow335-spellqueue-patcher .
```

Cross-compile the Windows binary from Linux:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -o wow335-spellqueue-patcher.exe .
```

Local builds report `dev`. To stamp a release version, add `-ldflags '-X main.version=1.0.2'`. Official releases use the Go version and build flags pinned in [.github/workflows/ci.yml](.github/workflows/ci.yml); use that toolchain and the tagged source to reproduce the binaries.

An optional integration check uses your own pristine client without writing a patched executable:

```bash
WOW_EXE=/path/to/Wow.original-3.3.5.12340.exe go test -run TestPristineClientIntegration -v
```

## CI and release downloads

GitHub Actions checks formatting, runs `go vet` and the tests, and cross-builds Linux/amd64 and Windows/amd64 on pushes and pull requests. Version tags publish both executables and `SHA256SUMS` after those checks pass. Client executables are never included in CI; the integration check skips unless `WOW_EXE` is supplied locally.

Download `SHA256SUMS` alongside the binaries to verify their hashes (`sha256sum --check --ignore-missing SHA256SUMS` on Linux, or `Get-FileHash .\wow335-spellqueue-patcher.exe -Algorithm SHA256` on Windows and compare its value). Checksums identify the published bytes; they are not code signing. Releases upload only the two patcher executables and their checksum manifest. GitHub provides source ZIP/tarball downloads automatically.

## License

The patcher and its documentation are MIT licensed. World of Warcraft and Blizzard Entertainment are trademarks of their respective owner; no affiliation or endorsement is implied.

See [AI_DISCLOSURE.md](AI_DISCLOSURE.md) for how AI assistance and human supervision contributed to this project.
