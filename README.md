# WoW 3.3.5a Spell Queue Patcher

This patch lets a WoW 3.3.5a build 12340 client send a spell request while a cast or global cooldown is still active. A compatible private server can then queue a request made near the end of the current cast instead of the client rejecting the keypress locally.

The patch does **not** remove the global cooldown, shorten casts, or make the client decide which casts are legal. AzerothCore remains authoritative. A request made inside `SpellQueue.Window` is queued; one made too early is rejected by the server.

## Before you begin

- Use a Windows WoW 3.3.5a client reporting build 12340.
- Back up `Wow.exe`. The patcher creates a new file and never overwrites its input.
- Your server must support and enable spell queueing. For AzerothCore:

  ```ini
  SpellQueue.Enabled = 1
  SpellQueue.Window = 400
  ```

  This project was tested with a 1000 ms window to make diagnosis obvious. A smaller value such as 400 ms is closer to the intended near-end queue behavior.

No Blizzard client files are included in this project.

## Windows

Put `wow335-spellqueue-patcher.exe` beside `Wow.exe`, open a terminal in that directory, and run:

```powershell
.\wow335-spellqueue-patcher.exe --check .\Wow.exe
.\wow335-spellqueue-patcher.exe --output .\Wow-spellqueue.exe .\Wow.exe
```

Keep the original `Wow.exe` as your backup. To use the patched client, either launch `Wow-spellqueue.exe` directly or rename the files so the patched copy is named `Wow.exe`.

The patcher is an unsigned community executable, so Windows may show a reputation warning. Build it from the short Go source if you do not want to run the supplied binary.

## Linux

The Linux patcher modifies the Windows client before it is launched through Wine or Proton:

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

We proved this behavior repeatedly against AzerothCore with Fireball. The patched client transmitted the early request, AzerothCore queued it, and the next cast began without interrupting the active cast.

## Compatibility and safety

The patcher accepts PE32/i386 WoW 3.3.5a build 12340 clients only when both surrounding code signatures occur exactly once. It supports:

- the pristine client used during development;
- the same client with unrelated RCE-prevention, large-address-aware, camera, and other static patches;
- partially patched and already patched copies.

It deliberately refuses an unknown, ambiguous, or structurally incompatible executable. A static offset alone would be unsafe because another client modification can move or replace code. The signatures identify the instructions by their surrounding machine code instead.

This is intended for compatible private servers. It does not make the 3.3.5a client compatible with modern official servers. See [TECHNICAL.md](TECHNICAL.md) for hashes, exact hex edits, reverse-engineering notes, failed approaches, and reproduction instructions.

## Building from source

Install Go 1.22 or newer, then run:

```bash
go test ./...
go build -trimpath -o wow335-spellqueue-patcher .
```

Cross-compile the Windows binary from Linux:

```bash
GOOS=windows GOARCH=amd64 go build -trimpath -o wow335-spellqueue-patcher.exe .
```

## License

The patcher and its documentation are MIT licensed. World of Warcraft and Blizzard Entertainment are trademarks of their respective owner; no affiliation or endorsement is implied.

See [AI_DISCLOSURE.md](AI_DISCLOSURE.md) for how AI assistance and human supervision contributed to this project.
