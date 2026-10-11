package collectors

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectLoginUsersFiltersSystemAndShells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	content := strings.Join([]string{
		"root:x:0:0:root:/root:/bin/bash",
		"daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin",
		"svc:x:999:999::/opt/svc:/bin/false",
		"alice:x:1000:1000:Alice:/home/alice:/bin/bash",
		"bob:x:1001:1001:Bob:/home/bob:/usr/sbin/nologin",
		"carol:x:1002:1002:Carol:/home/carol:/bin/zsh",
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := passwdPath
	passwdPath = path
	defer func() { passwdPath = old }()

	got := CollectLoginUsers(1000, 50)
	want := []LoginUser{
		{Name: "alice", UID: 1000, Home: "/home/alice"},
		{Name: "carol", UID: 1002, Home: "/home/carol"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCollectLoginUsersCapsAndMissingFile(t *testing.T) {
	old := passwdPath
	defer func() { passwdPath = old }()

	passwdPath = filepath.Join(t.TempDir(), "absent")
	if got := CollectLoginUsers(1000, 50); len(got) != 0 {
		t.Fatalf("missing passwd should yield no users, got %#v", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "u"+string(rune('a'+i))+":x:"+itoa(1000+i)+":1000::/home/u:/bin/bash")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	passwdPath = path
	if got := CollectLoginUsers(1000, 3); len(got) != 3 {
		t.Fatalf("cap not applied: got %d", len(got))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
