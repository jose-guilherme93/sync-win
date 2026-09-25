package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lem/agent/collectors"
)

func TestScanSaveCandidatesEncodesBinaryAndSkipsUnchanged(t *testing.T) {
	home := t.TempDir()
	textPath := filepath.Join(home, "slot.sav")
	binaryPath := filepath.Join(home, "slot.dat")
	if err := os.WriteFile(textPath, []byte("plain save"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte{0, 1, 2, 255}, 0o600); err != nil {
		t.Fatal(err)
	}
	state := &agentState{LastSaveSyncHashes: map[string]string{}}
	payloads, hashes := scanSaveCandidates([]collectors.SaveCandidate{
		{Path: textPath, RelativePath: "slot.sav", SizeBytes: 10},
		{Path: binaryPath, RelativePath: "slot.dat", SizeBytes: 4},
	}, state)
	if len(payloads) != 2 || len(hashes) != 2 {
		t.Fatalf("unexpected scan result: payloads=%#v hashes=%#v", payloads, hashes)
	}
	if payloads[0].Encoding != "" || payloads[0].Content != "plain save" {
		t.Fatalf("text payload = %#v", payloads[0])
	}
	if payloads[1].Encoding != "base64" || payloads[1].Content != base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 255}) {
		t.Fatalf("binary payload = %#v", payloads[1])
	}
	state.LastSaveSyncHashes["slot.sav"] = hashes["slot.sav"]
	payloads, _ = scanSaveCandidates([]collectors.SaveCandidate{{Path: textPath, RelativePath: "slot.sav", SizeBytes: 10}}, state)
	if len(payloads) != 0 {
		t.Fatalf("unchanged save was not skipped: %#v", payloads)
	}
}

func TestChunkSavePayloadsRespectsRequestLimit(t *testing.T) {
	payload := preferencePayload{Category: "saves", Filename: "slot.sav", RelativePath: "slot.sav", Content: strings.Repeat("a", 900*1024), Encoding: "base64"}
	chunks, err := chunkSavePayloads([]preferencePayload{payload, payload, payload}, "token")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) > maxSaveBatchItems || requestSize("token", chunk) > int(lemContract.ServerLimitsMirrored.MaxRequestBytes) {
			t.Fatalf("chunk exceeds limits: %#v", chunk)
		}
	}
}

func TestRestoreSavesWritesAtomicallyAndRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload, err := json.Marshal(restoreCommandPayload{Files: []restoreSaveFilePayload{
		{RelativePath: ".config/hydralauncher/prefix/slot.sav", Content: base64.StdEncoding.EncodeToString([]byte{0, 1, 2}), Encoding: "base64"},
		{RelativePath: "notes.txt", Content: "restored"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	count, err := restoreSaves(string(payload))
	if err != nil || count != 2 {
		t.Fatalf("restore count=%d err=%v", count, err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "hydralauncher", "prefix", "slot.sav"))
	if err != nil || len(data) != 3 || data[0] != 0 {
		t.Fatalf("restored binary = %v, err=%v", data, err)
	}
	if _, err := restoreSaves(`{"files":[{"relative_path":"../escape","content":"bad"}]}`); err == nil {
		t.Fatal("traversal restore path was accepted")
	}
}

func TestRestoreSavesRejectsSymlinkParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "linked")); err != nil {
		t.Fatal(err)
	}
	_, err := restoreSaves(`{"files":[{"relative_path":"linked/slot.sav","content":"bad"}]}`)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink parent was not rejected: %v", err)
	}
}
