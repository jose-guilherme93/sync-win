package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeLoginUsersDropsInvalidAndDedupes(t *testing.T) {
	got := normalizeLoginUsers(LoginUsers{
		Enabled:     true,
		DefaultUser: " alice ",
		Logins: []string{
			"bob", "", "alice ", "bob",
			"bad name", "../etc/passwd", "-flag", strings.Repeat("x", 65),
		},
	})
	if got.DefaultUser != "alice" {
		t.Fatalf("default user = %q, want alice", got.DefaultUser)
	}
	if !got.Enabled {
		t.Fatal("enabled flag lost")
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(got.Logins, want) {
		t.Fatalf("logins = %#v, want %#v", got.Logins, want)
	}
}
