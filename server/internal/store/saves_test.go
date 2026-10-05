package store

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSaveBinaryEncodingAndGameRestorePayload(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	device, err := store.RegisterDevice("pc", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := store.ListFiles(device.ID)
	if err != nil || empty == nil {
		t.Fatalf("empty file list must encode as []: %#v err=%v", empty, err)
	}

	binary := base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 255})
	saved, rejected, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{
		{Category: "saves", Filename: "slot.sav", RelativePath: ".config/hydralauncher/wine-prefixes/123/drive_c/users/u/Documents/Game/Sub/slot.sav", Content: binary, Encoding: "base64"},
		{Category: "saves", Filename: "other.sav", RelativePath: ".config/hydralauncher/wine-prefixes/999/drive_c/users/u/Documents/Other/other.sav", Content: "other"},
	})
	if err != nil || len(rejected) != 0 || len(saved) != 2 {
		t.Fatalf("save result saved=%d rejected=%#v err=%v", len(saved), rejected, err)
	}
	if saved[0].SizeBytes != 4 || saved[0].Encoding != "base64" {
		t.Fatalf("binary metadata = %#v", saved[0])
	}
	files, err := store.ListFiles(device.ID)
	if err != nil || len(files) != 2 || files[0].Encoding != "base64" {
		t.Fatalf("listed files = %#v err=%v", files, err)
	}

	matching, err := store.GetSaveFilesForGame(device.ID, "123", "Game/Sub")
	if err != nil || len(matching) != 1 || matching[0].Filename != "slot.sav" {
		t.Fatalf("matching files = %#v err=%v", matching, err)
	}
	command, err := store.QueueRestoreSavesWithFiles(device.ID, "source", "123", "Game/Sub", matching)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.GetCommand(command.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	var payload RestoreSavesPayload
	if err := json.Unmarshal([]byte(pending.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SourceDeviceID != "source" || len(payload.Files) != 1 || payload.Files[0].Encoding != "base64" {
		t.Fatalf("restore payload = %#v", payload)
	}
}

func TestSaveBinaryValidationRejectsInvalidBase64(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	device, err := store.RegisterDevice("pc", "user", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	_, rejected, err := store.SavePreferenceBatch(device.ID, []PreferenceInput{{
		Category: "saves", Filename: "slot.sav", RelativePath: "slot.sav", Content: "not-base64", Encoding: "base64",
	}})
	if err != nil || len(rejected) != 1 {
		t.Fatalf("invalid base64 result rejected=%#v err=%v", rejected, err)
	}
}
