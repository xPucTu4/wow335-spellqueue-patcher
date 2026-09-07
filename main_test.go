package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixture(first, second []byte, duplicate bool) []byte {
	text := append([]byte("WoW [Release] Build 12340\x003.3.5\x00"), bytes.Repeat([]byte{0xCC}, 32)...)
	add := func(site patchSite, branch []byte) {
		text = append(text, site.prefix...)
		text = append(text, branch...)
		text = append(text, site.suffix...)
		text = append(text, bytes.Repeat([]byte{0xCC}, 32)...)
	}
	add(patchSites[0], first)
	add(patchSites[1], second)
	if duplicate {
		add(patchSites[0], first)
	}

	rawSize := (len(text) + 0x1FF) &^ 0x1FF
	data := make([]byte, 0x200+rawSize)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[0x3C:], 0x80)
	copy(data[0x80:], "PE\x00\x00")
	fileHeader := 0x84
	binary.LittleEndian.PutUint16(data[fileHeader:], 0x14C)
	binary.LittleEndian.PutUint16(data[fileHeader+2:], 1)
	binary.LittleEndian.PutUint16(data[fileHeader+16:], 0xE0)
	binary.LittleEndian.PutUint16(data[fileHeader+18:], 0x102)
	optional := fileHeader + 20
	binary.LittleEndian.PutUint16(data[optional:], 0x10B)
	binary.LittleEndian.PutUint32(data[optional+16:], 0x1000)
	binary.LittleEndian.PutUint32(data[optional+20:], 0x1000)
	binary.LittleEndian.PutUint32(data[optional+28:], 0x400000)
	binary.LittleEndian.PutUint32(data[optional+32:], 0x1000)
	binary.LittleEndian.PutUint32(data[optional+36:], 0x200)
	binary.LittleEndian.PutUint32(data[optional+56:], 0x2000)
	binary.LittleEndian.PutUint32(data[optional+60:], 0x200)
	binary.LittleEndian.PutUint16(data[optional+68:], 2)
	binary.LittleEndian.PutUint32(data[optional+92:], 16)
	section := optional + 0xE0
	copy(data[section:], ".text")
	binary.LittleEndian.PutUint32(data[section+8:], uint32(len(text)))
	binary.LittleEndian.PutUint32(data[section+12:], 0x1000)
	binary.LittleEndian.PutUint32(data[section+16:], uint32(rawSize))
	binary.LittleEndian.PutUint32(data[section+20:], 0x200)
	binary.LittleEndian.PutUint32(data[section+36:], 0x60000020)
	copy(data[0x200:], text)
	return data
}

func TestPatchStates(t *testing.T) {
	tests := []struct {
		name   string
		first  []byte
		second []byte
		before []patchState
	}{
		{"unpatched", patchSites[0].original, patchSites[1].original, []patchState{unpatched, unpatched}},
		{"partial", patchSites[0].replacement, patchSites[1].original, []patchState{patched, unpatched}},
		{"patched", patchSites[0].replacement, patchSites[1].replacement, []patchState{patched, patched}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := fixture(test.first, test.second, false)
			before, err := inspect(data)
			if err != nil {
				t.Fatal(err)
			}
			for index, state := range test.before {
				if before.sites[index].state != state {
					t.Fatalf("site %d state = %s, want %s", index, before.sites[index].state, state)
				}
			}
			result, err := apply(data, before)
			if err != nil {
				t.Fatal(err)
			}
			after, err := inspect(result)
			if err != nil {
				t.Fatal(err)
			}
			if !allPatched(after) {
				t.Fatal("output is not fully patched")
			}
		})
	}
}

func TestRefusesInvalidAndAmbiguousInputs(t *testing.T) {
	if _, err := inspect([]byte("not a PE")); err == nil {
		t.Fatal("invalid input was accepted")
	}
	if _, err := inspect(fixture(patchSites[0].original, patchSites[1].original, true)); err == nil {
		t.Fatal("duplicate signature was accepted")
	}
	corrupt := fixture([]byte{0x12, 0x34}, patchSites[1].original, false)
	if _, err := inspect(corrupt); err == nil {
		t.Fatal("unexpected target bytes were accepted")
	}
	wrongBuild := bytes.ReplaceAll(fixture(patchSites[0].original, patchSites[1].original, false), []byte("12340"), []byte("12341"))
	if _, err := inspect(wrongBuild); err == nil {
		t.Fatal("wrong build marker was accepted")
	}
	nearMiss := fixture(patchSites[0].original, patchSites[1].original, true)
	matches := findSite(nearMiss, patchSites[0])
	nearMiss[matches[1]+len(patchSites[0].prefix)] = 0xCC
	if _, err := inspect(nearMiss); err == nil {
		t.Fatal("ambiguous surrounding signatures were accepted")
	}
}

func TestPreservesUnrelatedBytes(t *testing.T) {
	data := fixture(patchSites[0].original, patchSites[1].original, false)
	before, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	result, err := apply(data, before)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("input was modified")
	}
	allowed := map[int]bool{}
	for _, site := range before.sites {
		for index := range site.site.original {
			if site.site.original[index] != site.site.replacement[index] {
				allowed[site.fileOffset+index] = true
			}
		}
	}
	for index := range original {
		if original[index] != result[index] && !allowed[index] {
			t.Fatalf("unrelated byte changed at 0x%X", index)
		}
	}
}

func TestKnownOutputMismatch(t *testing.T) {
	data := fixture(patchSites[0].original, patchSites[1].original, false)
	before, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	before.hash = "aa63a5750d60ef16746c686b3d5e26876d98953eab08b1c026cd0faf78e88cb8"
	if _, err := apply(data, before); err == nil || !strings.Contains(err.Error(), "known-input output hash mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

// Run the real CLI entrypoint in a subprocess so exit codes and both streams are checked.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("PATCHER_TEST_PROCESS") == "1" {
		os.Args = append([]string{os.Args[0]}, os.Args[3:]...)
		main()
		os.Exit(0)
	}
}

func TestCLI(t *testing.T) {
	for _, name := range []string{"help", "bad-flag", "version", "check", "already-patched", "in-place", "collision", "write"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "Wow.exe"), filepath.Join(dir, "Wow-spellqueue.exe")
			data := fixture(patchSites[0].original, patchSites[1].original, false)
			args := []string{input}
			wantExit, wantOut, wantErr := 0, "", ""
			switch name {
			case "help":
				args, wantOut = []string{"--help"}, "Usage of wow335-spellqueue-patcher:"
			case "bad-flag":
				args, wantExit, wantErr = []string{"--unknown"}, 1, "Error: flag provided but not defined: -unknown"
			case "version":
				args, wantOut = []string{"--version"}, "wow335-spellqueue-patcher "+version
			case "check":
				args, wantOut = []string{"--check", input}, "compatible and ready to patch"
			case "already-patched":
				data = fixture(patchSites[0].replacement, patchSites[1].replacement, false)
				args, wantOut = []string{"--output", output, input}, "already patched; no output written"
			case "in-place":
				args, wantExit, wantErr = []string{"--output", input, input}, 1, "refusing to patch in place"
			case "collision":
				if err := os.WriteFile(output, []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
				wantExit, wantErr = 1, "output already exists; choose another --output"
			case "write":
				wantOut = "Created: " + output
			}
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "PATCHER_TEST_PROCESS=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
			}
			if cmd.ProcessState.ExitCode() != wantExit || !strings.Contains(stdout.String(), wantOut) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", cmd.ProcessState.ExitCode(), stdout.String(), stderr.String())
			}
			if (wantErr == "" && stderr.Len() != 0) || (wantErr != "" && strings.Count(stderr.String(), wantErr) != 1) {
				t.Fatalf("unexpected stderr: %q", stderr.String())
			}
			if name == "bad-flag" && stderr.String() != wantErr+"\n" {
				t.Fatalf("duplicate flag diagnostics: %q", stderr.String())
			}
			unchanged, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(unchanged, data) {
				t.Fatalf("input changed: %v", err)
			}
			written, err := os.ReadFile(output)
			switch name {
			case "write":
				expected := fixture(patchSites[0].replacement, patchSites[1].replacement, false)
				if err != nil || !bytes.Equal(written, expected) {
					t.Fatalf("incorrect output: %v", err)
				}
			case "collision":
				if err != nil || string(written) != "keep me" {
					t.Fatalf("existing output changed: %v", err)
				}
			default:
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unexpected output file: %v", err)
				}
			}
		})
	}
}

func TestPristineClientIntegration(t *testing.T) {
	path := os.Getenv("WOW_EXE")
	if path == "" {
		t.Skip("set WOW_EXE to the documented pristine build-12340 executable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest(data) != "aa63a5750d60ef16746c686b3d5e26876d98953eab08b1c026cd0faf78e88cb8" {
		t.Fatal("WOW_EXE must be the documented pristine input")
	}
	original := bytes.Clone(data)
	before, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := apply(data, before)
	if err != nil {
		t.Fatal(err)
	}
	if digest(result) != "47073149dff70ac1e06fa9aa600935181620e6449fd01538945ff47c710678a5" {
		t.Fatal("patched hash differs from the documented output")
	}
	var changed []int
	for index := range data {
		if data[index] != result[index] {
			changed = append(changed, index)
		}
	}
	if !slices.Equal(changed, []int{0x338B7D, 0x40C777, 0x40C778}) {
		t.Fatalf("unexpected changed offsets: %X", changed)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, data) || !bytes.Equal(original, onDisk) {
		t.Fatalf("input changed: %v", err)
	}
}
