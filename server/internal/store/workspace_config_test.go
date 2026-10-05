package store

import "testing"

func TestWorkspaceDirsSyncConfig(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetWorkspaceDirs("owner", []string{"/home/user/project"}); err != nil {
		t.Fatal(err)
	}
	dirs, err := store.GetWorkspaceDirs("owner")
	if err != nil || len(dirs) != 1 || dirs[0] != "/home/user/project" {
		t.Fatalf("workspace dirs = %#v err=%v", dirs, err)
	}
	if err := store.SetSyncConfig("owner", []string{"/home/user/saves"}); err != nil {
		t.Fatal(err)
	}
	dirs, err = store.GetWorkspaceDirs("owner")
	if err != nil || len(dirs) != 1 {
		t.Fatalf("SetSyncConfig changed workspace dirs: %#v err=%v", dirs, err)
	}
	if err := store.SetWorkspaceDirs("owner", []string{"../escape"}); err == nil {
		t.Fatal("workspace traversal path was accepted")
	}
}
