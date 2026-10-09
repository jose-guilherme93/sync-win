package collectors

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// passwdPath is a variable so tests can point at a fixture.
var passwdPath = "/etc/passwd"

// LoginUser is a real account a remote session may log in as. Only the name, uid
// and home are read from /etc/passwd; no shell history or user data is touched.
type LoginUser struct {
	Name string `json:"name"`
	UID  int    `json:"uid"`
	Home string `json:"home,omitempty"`
}

// CollectLoginUsers enumerates the accounts offered as remote-access targets:
// uid >= minUID, a real login shell, and no system role. `nologin`/`false`
// shells, system accounts and root are excluded (root is out of scope for now).
// A missing or unreadable /etc/passwd yields an empty list, never an error.
func CollectLoginUsers(minUID, max int) []LoginUser {
	if minUID <= 0 {
		minUID = 1000
	}
	if max <= 0 {
		max = 50
	}
	data, err := os.ReadFile(passwdPath)
	if err != nil {
		return []LoginUser{}
	}
	users := make([]LoginUser, 0, 8)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		name := fields[0]
		if name == "" || name == "nobody" || name == "root" {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < minUID {
			continue
		}
		shell := fields[6]
		if shell == "" || strings.HasSuffix(shell, "nologin") || strings.HasSuffix(shell, "false") {
			continue
		}
		users = append(users, LoginUser{Name: name, UID: uid, Home: fields[5]})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].UID < users[j].UID })
	if len(users) > max {
		users = users[:max]
	}
	return users
}
