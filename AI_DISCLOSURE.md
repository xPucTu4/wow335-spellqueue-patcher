# AI Disclosure

This project was developed with substantial AI assistance.

The AI performed most of the binary-analysis work: proposing experiments, reading disassembly and processor traces, narrowing the client-side rejection path, identifying the two conditional branches, implementing the patcher, and drafting the documentation.

That work happened under extensive supervision by an experienced software developer. The developer had less prior experience in reverse engineering, but directed the investigation, reviewed the conclusions, controlled every live client and server test, reported the observed behavior, and decided what was safe to keep or reject.

The result was not accepted from static analysis alone. Failed candidates were discarded after testing, the final three-byte patch was exercised with both a pristine client and an independently patched client, and server logs were used to verify packet receipt, queueing, rejection, completion, and cancellation behavior.

AI involvement does not guarantee correctness. Users should review the source and technical reproduction notes, keep an original client backup, and apply the patch only to supported WoW 3.3.5a build 12340 executables.
