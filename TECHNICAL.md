# Technical Analysis and Reproduction

## Result

The stock WoW 3.3.5a build 12340 client normally consumes some spell keypresses locally while a cast or global cooldown is active. Since no `CMSG_CAST_SPELL` reaches AzerothCore, changing only `SpellQueue.Window` cannot make those attempts queue.

The working client patch bypasses two local cooldown-only diversions. It changes three bytes in total and lets execution continue through the client's normal cast validation and packet construction. AzerothCore still accepts or rejects the request according to its normal spell rules and queue window.

The bypasses are unconditional, including for spell cooldowns much longer than the queue window. Repeated attempts that pass the remaining client checks can therefore send additional `CMSG_CAST_SPELL` packets for an unavailable spell. The corresponding cooldown rejection depends on a server round trip, replacing the immediate local error on these paths. This patch does not synthesize keypresses or packets, and it does not guarantee that every keypress reaches packet construction.

With `SpellQueue.Enabled = 0`, AzerothCore does not accept queue requests; its ordinary cast validation still rejects unavailable casts. The patched client retains the extra requests and server-dependent feedback without a queueing benefit.

## Exact patch

The addresses below apply to the tested PE32 executable with image base `0x00400000`, `.text` virtual address `0x00401000`, and `.text` raw file offset `0x400`.

| Purpose | File offset | Virtual address | Original | Patched |
|---|---:|---:|---|---|
| Bypass the silent cooldown diversion | `0x40C777` | `0x0080D377` | `75 0C` (`JNZ +0x0C`) | `90 90` (`NOP; NOP`) |
| Bypass the later local not-ready diversion | `0x338B7D` | `0x0073977D` | `74 20` (`JZ +0x20`) | `EB 20` (`JMP +0x20`) |

The offsets are useful for confirming the tested binaries, but the redistributable patcher does not trust them. It searches only the PE `.text` section for the surrounding signatures and requires exactly one occurrence of each.

It counts matching prefix/suffix contexts before checking the branch bytes. A second context with unfamiliar middle bytes is deliberately ambiguous, not silently discarded. This conservative refusal avoids choosing between a recognized site and another possibly modified site. Code relocated outside `.text` is not discovered; unrelated injected sections are not audited.

The PE checksum is left unchanged. The tested clients load with the unchanged checksum; recalculating it would add changes beyond the three documented byte positions.

### Hex-editor reproduction

1. Back up `Wow.exe` and verify that the client reports version 3.3.5a build 12340.
2. Search in hex mode for this exact sequence:

   ```text
   85 C0 75 0C F7 85 58 FD FF FF 04 04 00 00 74 49
   ```

3. Require exactly one match. Replace only `75 0C` with `90 90`. The result is:

   ```text
   85 C0 90 90 F7 85 58 FD FF FF 04 04 00 00 74 49
   ```

4. Search for this exact sequence:

   ```text
   83 C4 14 85 C0 74 20 8B 45 08 6A 00 6A FF 6A FF 6A 43
   ```

5. Require exactly one match. Replace only `74` with `EB`, leaving the displacement byte `20` unchanged. The result is:

   ```text
   83 C4 14 85 C0 EB 20 8B 45 08 6A 00 6A FF 6A FF 6A 43
   ```

6. Save to a new file. Compare the original and output: exactly three byte positions must differ. Do not apply the edit if either signature is absent, duplicated, or already contains other bytes.

This procedure is equivalent to the patcher and requires neither compiling nor executing it.

## Known hashes

| File | SHA-256 |
|---|---|
| Pristine Blizzard-provided input | `aa63a5750d60ef16746c686b3d5e26876d98953eab08b1c026cd0faf78e88cb8` |
| Pristine input plus this spell-queue patch | `47073149dff70ac1e06fa9aa600935181620e6449fd01538945ff47c710678a5` |
| Existing composite input with RCE/LAA/camera and other fixes | `94bbc08494283cae4d4c9cf8fe6272bf05cb01d03ca8a1466f13d19c4753248d` |
| Composite input plus this spell-queue patch | `cce863e67351fce30d6e818c67b579ac8815626406c6e3afe173fa8a29f52cec` |

Other compatible clients will naturally have different whole-file hashes. The two code signatures, version markers, PE architecture, unique-match rule, expected branch bytes, three-byte change audit, and output reinspection are the compatibility checks.

## How the rejection was located

Server-side logging first distinguished server rejection from client suppression. A normal Fireball generated `CMSG_CAST_SPELL` opcode `0x12E`, spell ID 133. A failed near-end keypress sometimes generated no second cast packet and no cancel packet at all. That proved the missing queue request had to be fixed before packet transmission.

Static analysis identified these useful build-12340 landmarks:

| Address | Role |
|---:|---|
| `0x005AC000` | Lua `UseAction` binding |
| `0x005ABBC0` | Native action dispatch |
| `0x0080CCE0` | Spell preparation path |
| `0x0080C790` | Target/cast route after preparation |
| `0x0080AC90` | Cast validation and packet construction |
| `0x0080B2F5` | `CMSG_CAST_SPELL` construction |
| `0x0080B4EE` | Cast packet send call |
| `0x006B0B50` | `ClientServices::SendPacket` |
| `0x008062FE` | `CMSG_CANCEL_CAST` send call |

A normal cast reached construction and send. The rejected second keypress reached `UseAction`, action dispatch, and `0x0080CCE0`, but returned before `0x0080C790`.

The first broad debugger attempt used 15 software breakpoints and caused the client to terminate when casting, without a useful dump. A focused single hardware breakpoint was stable, but the silent rejection never reached the suspected local cast-error function at `0x00808200`.

We then used a two-hardware-breakpoint Intel Processor Trace capture around only the second `0x0080CCE0` invocation. It recorded 32,120 instructions, 1,205 calls, and zero trace gaps. The rejected path returned at `0x0080D263`; immediately beforehand, code called the cooldown lookup at `0x00809000` for Fireball and diverted at `0x0080D377` when a local cooldown/GCD was active.

Neutralizing that first branch changed the symptom from a silent click to the spoken “spell is not ready yet” response, but still sent no packet. A focused cast-error trace then recorded result 67 (`SPELL_FAILED_NOT_READY`) from caller `0x00739791`, inside the spell-usability predicate beginning at `0x00739650`. Its dedicated branch is at `0x0073977D`. Bypassing that second cooldown-only diversion produced the working three-byte patch.

Disassembly of the documented pristine input shows one direct call to `0x00739650`, at `0x0080D6D1` in the preparation path. This is a direct-call observation, not proof that no indirect caller exists. The patch changes the cast-path branches rather than the shared cooldown query at `0x00809000`; it does not modify action-bar cooldown rendering code.

The attribute test immediately after the first patch site is `test [ebp-0x2A8], 0x404` at `0x0080D379`. The mask combines `SPELL_ATTR0_ON_NEXT_SWING_NO_DAMAGE` (`0x4`) and `SPELL_ATTR0_ON_NEXT_SWING` (`0x400`), the two on-next-melee-swing flags in AzerothCore's `SpellAttr0`. This attribute check remains intact: those spells can still take that special path after the cooldown branch is bypassed. It is not a blanket bypass of every preparation guard.

## Failed approaches and why

- **Increasing `SpellQueue.Window` alone:** ineffective when the client sends no opcode. Server policy cannot queue a request it never receives.
- **First guessed current-cast branch near `0x0080D306`:** the tested path reached that code with state that did not take the branch, so changing it did not alter the failure.
- **Only patching `0x0080D377`:** passed the silent diversion but exposed the later explicit `SPELL_FAILED_NOT_READY` path. The client changed feedback but still suppressed transmission.
- **Many software breakpoints:** too invasive for this Proton/Wine client and caused termination. Hardware breakpoints plus a bounded processor trace were stable.
- **Patching Lua or action-bar code:** too high-level. The keypress already reached native spell preparation; the rejection was in lower-level cooldown checks shared by the actual cast path.
- **Runtime DLL injection:** unnecessary for these fixed instructions and harder to redistribute across Linux/Wine and Windows. It still needs reliable code discovery, plus a loader, architecture-specific hooking, and executable-memory changes. Static signature patching has fewer moving parts.
- **A raw fixed-offset patcher:** unsafe for generally patched clients. Whole-file patches may change unrelated regions or layouts. Signature matching tolerates unrelated RCE, LAA, camera, and similar changes while refusing ambiguous code.
- **Suppressing all `CMSG_CANCEL_CAST`:** incorrect. It would break deliberate cancellation and hide a symptom. An early experimental cancel was observed once but was not reproduced by the final patch, so no server or cancel-path modification was needed.

## Why the patch preserves authority

Both changed branches are client-side responses to the local cooldown lookup. The patch does not jump directly to packet sending; it lets execution proceed through the existing usability, targeting, cast construction, and send path. Other failures remain in place.

In the recorded Fireball cast-chain tests, requests transmitted too early were rejected with `SPELL_FAILED_SPELL_IN_PROGRESS` (result 105) and the active cast continued. Requests inside the configured window were retained and replayed after the current spell completed. Other rejection reasons still depend on the server's normal validation. Timing policy remains entirely server-side.

### Why there is no client-side timing threshold

A conditional bypass based on remaining cooldown could reduce unnecessary requests and preserve immediate feedback for long cooldowns. It is a different, more involved patch: the call at `0x00739773` currently passes three zero arguments alongside the spell ID and the lookup flag. Using cooldown outputs would require verifying their meaning and units, supplying storage, adding timing comparisons and control flow, and choosing how the client threshold tracks each server's queue window. That cannot be obtained by the current three-byte branch edit.

This design deliberately accepts the extra requests in exchange for a small static patch and one server-owned timing policy. Conditional timing is a possible future alternative, not implemented or promised by this release.

## Runtime evidence

With the composite client and both edits applied:

- Fireball cast counters 1, 2, 3, and 4 formed an uninterrupted queued chain.
- Early requests 6, 9, 11, and 15 were rejected by the server with result 105 while active casts 5, 8, 10, and 14 completed normally.
- Later requests 7, 13, and 16 were accepted near the end and began after the active cast.
- Accepted queue requests appeared once when received and again when AzerothCore replayed the queued packet after `SMSG_SPELL_GO`.
- No final-test `CMSG_CANCEL_CAST` or interrupted-spell event appeared.

The redistributable patcher was then tested against both intended inputs:

- The pristine input produced hash `47073149...` and queued cast counter 2 behind cast counter 1. A later cast counter 4 received `CMSG_CANCEL_CAST` and was interrupted normally, confirming the patch does not suppress cancellation.
- The composite input produced hash `cce863e6...`, queued cast counter 2 behind cast counter 1, and completed the following cast counter 3 normally.
- The Windows patcher was executed under Wine and produced an output byte-for-byte identical to the Linux patcher output.

The developer also manually tested many other spells. The Fireball trace above is the detailed recorded example, not the only spell tested. No exhaustive spell-by-spell matrix is published, so these notes do not claim complete coverage of channels, autorepeat, on-next-melee-swing attacks, pet spells, item/trinket use, macros, or UI behavior.

## Patcher design

The implementation is a single Go program using only the standard library:

1. Parse the input as Windows PE.
2. Require i386 PE32, `.text`, and embedded build 12340/version 3.3.5 markers.
3. Require exactly one surrounding prefix/suffix context per site in `.text`, then require original or patched branch bytes.
4. Copy the input in memory and modify only unpatched sites.
5. Audit the changed offsets, reinspect the result, and verify known output hashes when the input hash is known.
6. Create a separate output using exclusive-create semantics and read it back to verify its digest.

The same source builds natively on Linux and Windows. It never patches in place and never distributes Blizzard code.

## Related upstream context

- [AzerothCore issue #9300: Spell Queue System](https://github.com/azerothcore/azerothcore-wotlk/issues/9300)
- [AzerothCore pull request #20797](https://github.com/azerothcore/azerothcore-wotlk/pull/20797)
- [Community list of 3.3.5 client projects and extensions](https://github.com/haha2345/awesome-wow-335-clients)
