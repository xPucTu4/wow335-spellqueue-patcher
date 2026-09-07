package main

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var version = "dev"

type patchState string

const (
	unpatched patchState = "unpatched"
	patched   patchState = "patched"
)

type patchSite struct {
	name        string
	prefix      []byte
	original    []byte
	replacement []byte
	suffix      []byte
}

type locatedSite struct {
	site       patchSite
	state      patchState
	fileOffset int
	virtual    uint32
}

type inspection struct {
	sites []locatedSite
	hash  string
}

var patchSites = []patchSite{
	{
		name:        "silent cooldown diversion",
		prefix:      hexBytes("85 c0"),
		original:    hexBytes("75 0c"),
		replacement: hexBytes("90 90"),
		suffix:      hexBytes("f7 85 58 fd ff ff 04 04 00 00 74 49"),
	},
	{
		name:        "SPELL_FAILED_NOT_READY diversion",
		prefix:      hexBytes("83 c4 14 85 c0"),
		original:    hexBytes("74 20"),
		replacement: hexBytes("eb 20"),
		suffix:      hexBytes("8b 45 08 6a 00 6a ff 6a ff 6a 43"),
	},
}

var knownOutputs = map[string]string{
	"aa63a5750d60ef16746c686b3d5e26876d98953eab08b1c026cd0faf78e88cb8": "47073149dff70ac1e06fa9aa600935181620e6449fd01538945ff47c710678a5",
	"94bbc08494283cae4d4c9cf8fe6272bf05cb01d03ca8a1466f13d19c4753248d": "cce863e67351fce30d6e818c67b579ac8815626406c6e3afe173fa8a29f52cec",
	"c2597b3e38ed77109e8c4a466b43eb4fac78e5632cfecf28b45a07fdb376bacc": "cce863e67351fce30d6e818c67b579ac8815626406c6e3afe173fa8a29f52cec",
}

func hexBytes(value string) []byte {
	result, err := hex.DecodeString(strings.ReplaceAll(value, " ", ""))
	if err != nil {
		panic(err)
	}
	return result
}

func digest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func inspect(data []byte) (inspection, error) {
	if len(data) < 2 || !bytes.Equal(data[:2], []byte("MZ")) {
		return inspection{}, errors.New("input is not a Windows PE executable")
	}
	file, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return inspection{}, fmt.Errorf("invalid PE executable: %w", err)
	}
	defer file.Close()

	if file.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return inspection{}, fmt.Errorf("unsupported PE machine 0x%X; expected i386", file.Machine)
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader32)
	if !ok {
		return inspection{}, errors.New("unsupported executable; expected PE32")
	}
	if !bytes.Contains(data, []byte("WoW [Release] Build 12340")) || !bytes.Contains(data, []byte("3.3.5")) {
		return inspection{}, errors.New("unsupported executable; WoW 3.3.5a build 12340 markers not found")
	}

	var text *pe.Section
	for _, section := range file.Sections {
		if section.Name == ".text" {
			text = section
			break
		}
	}
	if text == nil {
		return inspection{}, errors.New("invalid executable; .text section not found")
	}
	start, end := int(text.Offset), int(text.Offset+text.Size)
	if start < 0 || end < start || end > len(data) {
		return inspection{}, errors.New("invalid executable; .text section is outside the file")
	}

	result := inspection{hash: digest(data)}
	for _, site := range patchSites {
		matches := findSite(data[start:end], site)
		if len(matches) != 1 {
			return inspection{}, fmt.Errorf("%s signature matched %d times; refusing ambiguous executable", site.name, len(matches))
		}
		target := start + matches[0] + len(site.prefix)
		current := data[target : target+len(site.original)]
		state := unpatched
		switch {
		case bytes.Equal(current, site.original):
		case bytes.Equal(current, site.replacement):
			state = patched
		default:
			return inspection{}, fmt.Errorf("%s has unexpected bytes % X", site.name, current)
		}
		result.sites = append(result.sites, locatedSite{
			site:       site,
			state:      state,
			fileOffset: target,
			virtual:    optional.ImageBase + text.VirtualAddress + uint32(matches[0]+len(site.prefix)),
		})
	}
	return result, nil
}

func findSite(text []byte, site patchSite) []int {
	length := len(site.prefix) + len(site.original) + len(site.suffix)
	var matches []int
	for offset := 0; offset+length <= len(text); offset++ {
		target := offset + len(site.prefix)
		if bytes.Equal(text[offset:target], site.prefix) &&
			bytes.Equal(text[target+len(site.original):offset+length], site.suffix) {
			matches = append(matches, offset)
		}
	}
	return matches
}

func apply(data []byte, before inspection) ([]byte, error) {
	result := bytes.Clone(data)
	var expectedChanges []int
	for _, located := range before.sites {
		if located.state == patched {
			continue
		}
		copy(result[located.fileOffset:], located.site.replacement)
		for index := range located.site.original {
			if located.site.original[index] != located.site.replacement[index] {
				expectedChanges = append(expectedChanges, located.fileOffset+index)
			}
		}
	}

	var actualChanges []int
	for index := range data {
		if data[index] != result[index] {
			actualChanges = append(actualChanges, index)
		}
	}
	slices.Sort(expectedChanges)
	if !slices.Equal(actualChanges, expectedChanges) {
		return nil, fmt.Errorf("internal safety check failed: changed offsets %v, expected %v", actualChanges, expectedChanges)
	}
	after, err := inspect(result)
	if err != nil {
		return nil, fmt.Errorf("patched output failed validation: %w", err)
	}
	for _, located := range after.sites {
		if located.state != patched {
			return nil, fmt.Errorf("patched output left %s unpatched", located.site.name)
		}
	}
	if expected, ok := knownOutputs[before.hash]; ok && digest(result) != expected {
		return nil, fmt.Errorf("known-input output hash mismatch: got %s, expected %s", digest(result), expected)
	}
	return result, nil
}

func allPatched(value inspection) bool {
	for _, site := range value.sites {
		if site.state != patched {
			return false
		}
	}
	return true
}

func writeNew(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output already exists; choose another --output or remove the existing file first: %w", err)
		}
		return err
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	verified, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if digest(verified) != digest(data) {
		return errors.New("output verification failed")
	}
	ok = true
	return nil
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("wow335-spellqueue-patcher", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	check := flags.Bool("check", false, "validate and report without writing an output")
	output := flags.String("output", "", "output path (default: Wow-spellqueue.exe beside input)")
	showVersion := flags.Bool("version", false, "print version")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(os.Stdout)
			flags.Usage()
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Printf("wow335-spellqueue-patcher %s\n", version)
		return nil
	}
	if flags.NArg() > 1 {
		return errors.New("usage: wow335-spellqueue-patcher [--check] [--output FILE] [INPUT]")
	}
	input := "Wow.exe"
	if flags.NArg() == 1 {
		input = flags.Arg(0)
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	info, err := inspect(data)
	if err != nil {
		return err
	}

	fmt.Printf("Compatible WoW 3.3.5a build 12340 PE32/i386\nInput SHA-256: %s\n", info.hash)
	for _, site := range info.sites {
		fmt.Printf("- %s: %s at file 0x%X, VA 0x%08X\n", site.site.name, site.state, site.fileOffset, site.virtual)
	}
	if *check || allPatched(info) {
		if allPatched(info) {
			fmt.Println("Result: already patched; no output written")
		} else {
			fmt.Println("Result: compatible and ready to patch")
		}
		return nil
	}

	patchedData, err := apply(data, info)
	if err != nil {
		return err
	}
	destination := *output
	if destination == "" {
		destination = filepath.Join(filepath.Dir(input), "Wow-spellqueue.exe")
	}
	inputAbs, _ := filepath.Abs(input)
	outputAbs, _ := filepath.Abs(destination)
	if inputAbs == outputAbs {
		return errors.New("refusing to patch in place; choose a separate output")
	}
	stat, err := os.Stat(input)
	if err != nil {
		return err
	}
	if err = writeNew(destination, patchedData, stat.Mode()); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	fmt.Printf("Created: %s\nOutput SHA-256: %s\n", destination, digest(patchedData))
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
