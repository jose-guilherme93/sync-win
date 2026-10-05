package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sync-win/agent/collectors"
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
		if len(chunk) > maxSaveBatchItems || requestSize("token", chunk) > int(syncwinContract.ServerLimitsMirrored.MaxRequestBytes) {
			t.Fatalf("chunk exceeds limits: %#v", chunk)
		}
	}
}

func TestRestoreSavesWritesAtomicallyAndRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prefixes := allowedRestorePrefixes(nil, home)
	payload, err := json.Marshal(restoreCommandPayload{Files: []restoreSaveFilePayload{
		{RelativePath: ".steam/steam/userdata/1/2/remote/slot.sav", Content: base64.StdEncoding.EncodeToString([]byte{0, 1, 2}), Encoding: "base64"},
		{RelativePath: ".config/unity3d/Game/notes.txt", Content: "restored"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	count, err := restoreSaves(string(payload), prefixes)
	if err != nil || count != 2 {
		t.Fatalf("restore count=%d err=%v", count, err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".steam", "steam", "userdata", "1", "2", "remote", "slot.sav"))
	if err != nil || len(data) != 3 || data[0] != 0 {
		t.Fatalf("restored binary = %v, err=%v", data, err)
	}
	if _, err := restoreSaves(`{"files":[{"relative_path":"../escape","content":"bad"}]}`, prefixes); err == nil {
		t.Fatal("traversal restore path was accepted")
	}
}

func TestRestoreSavesRejectsPathsOutsideSaveRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prefixes := allowedRestorePrefixes(nil, home)
	for _, rel := range []string{".bashrc", ".config/sync-win/policy.json", ".config/sync-win/allowed-files", ".ssh/authorized_keys", "notes.txt"} {
		payload := `{"files":[{"relative_path":"` + rel + `","content":"pwned"}]}`
		if _, err := restoreSaves(payload, prefixes); err == nil {
			t.Fatalf("restore to %q was accepted", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		t.Fatal("sensitive file was written")
	}
}

func TestAllowedRestorePrefixesIncludesExtraDirs(t *testing.T) {
	home := t.TempDir()
	extra := filepath.Join(home, "GameSaves")
	prefixes := allowedRestorePrefixes([]string{extra}, home)
	if !restorePathAllowed("GameSaves/foo.sav", prefixes) {
		t.Fatal("operator extra dir prefix was not allowed")
	}
	if restorePathAllowed("Other/foo.sav", prefixes) {
		t.Fatal("unrelated path was allowed")
	}
}

func TestRestoreSavesRejectsSymlinkParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prefixes := allowedRestorePrefixes(nil, home)
	outside := t.TempDir()
	// .config/unity3d is a configured save root; make it a symlink so the write
	// would escape home.
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".config", "unity3d")); err != nil {
		t.Fatal(err)
	}
	_, err := restoreSaves(`{"files":[{"relative_path":".config/unity3d/slot.sav","content":"bad"}]}`, prefixes)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink parent was not rejected: %v", err)
	}
}
