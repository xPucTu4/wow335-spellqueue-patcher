package main

import (
	"bytes"
	"encoding/binary"
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
