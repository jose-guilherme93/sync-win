package remote

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// marker returns the comment token appended to a managed key line. Revocation
// removes every line that carries it, so a session cannot leave a stray key
// behind even if the file was edited meanwhile.
func marker(id string) string {
	return "syncwin:" + id
}

// validatePublicKey canonicalizes a public key down to "type base64", dropping
// any comment. Rebuilding from validated fields is what stops a malicious key
// from smuggling authorized_keys options or extra lines.
func validatePublicKey(key, wantType string) (string, error) {
	if strings.ContainsAny(key, "\r\n") {
		return "", errors.New("public key must be a single line")
	}
	fields := strings.Fields(key)
	if len(fields) < 2 {
		return "", errors.New("malformed public key")
	}
	if wantType != "" && fields[0] != wantType {
		return "", fmt.Errorf("unsupported key type %q", fields[0])
	}
	raw, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "", errors.New("public key is not valid base64")
	}
	// An ed25519 public key blob is type prefix (11) + 32 key bytes. Anything
	// much smaller is not a key.
	if len(raw) < 16 {
		return "", errors.New("public key is too short")
	}
	return fields[0] + " " + fields[1], nil
}

// composeAuthorizedKeyLine builds the exact line the helper will append.
func composeAuthorizedKeyLine(keyType, key, options, id string, expiry time.Time) (string, error) {
	canonical, err := validatePublicKey(key, keyType)
	if err != nil {
		return "", err
	}
	opts := strings.TrimSpace(options)
	if opts != "" && !strings.HasSuffix(opts, ",") {
		opts += ","
	}
	opts += fmt.Sprintf(`expiry-time="%s"`, expiry.UTC().Format("20060102150405"))
	return fmt.Sprintf("%s %s %s", opts, canonical, marker(id)), nil
}

func filterMarkerLines(content, id string) string {
	token := marker(id)
	var kept []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, token) {
			continue
		}
		kept = append(kept, line)
	}
	// Drop the empty element produced by a trailing newline, then rejoin.
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}

// InjectFile appends a line to an authorized_keys file, replacing any prior line
// with the same session marker. It is idempotent and atomic.
func InjectFile(path, line, id string, uid, gid int) error {
	content, err := readIfExists(path)
	if err != nil {
		return err
	}
	content = filterMarkerLines(content, id) + line + "\n"
	return writeFileAtomic(path, []byte(content), 0o600, uid, gid)
}

// RevokeFile removes every line carrying the session marker.
func RevokeFile(path, id string, uid, gid int) error {
	content, err := readIfExists(path)
	if err != nil {
		return err
	}
	filtered := filterMarkerLines(content, id)
	if filtered == content {
		return nil
	}
	return writeFileAtomic(path, []byte(filtered), 0o600, uid, gid)
}

func readIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode, uid, gid int) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".authkeys-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if uid >= 0 && gid >= 0 {
		if err := tmp.Chown(uid, gid); err != nil {
			cleanup()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// lookupUser resolves a user name to its home, uid and gid.
func lookupUser(name string) (home string, uid, gid int, err error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", 0, 0, err
	}
	if u.HomeDir == "" || !filepath.IsAbs(u.HomeDir) {
		return "", 0, 0, fmt.Errorf("user %q has no absolute home directory", name)
	}
	uid, uidErr := strconv.Atoi(u.Uid)
	gid, gidErr := strconv.Atoi(u.Gid)
	if uidErr != nil || gidErr != nil {
		return "", 0, 0, fmt.Errorf("user %q has a non-numeric uid/gid", name)
	}
	return u.HomeDir, uid, gid, nil
}

// AuthorizedKeysPath returns ~/.ssh/authorized_keys for a user.
func AuthorizedKeysPath(name string) (string, int, int, error) {
	home, uid, gid, err := lookupUser(name)
	if err != nil {
		return "", 0, 0, err
	}
	return filepath.Join(home, ".ssh", "authorized_keys"), uid, gid, nil
}

// InjectUser is the root-side inject: it resolves the target user, ensures
// ~/.ssh exists with the right ownership, and adds the restricted key line.
func InjectUser(name, key, keyType, options string, id string, ttl time.Duration) error {
	path, uid, gid, err := AuthorizedKeysPath(name)
	if err != nil {
		return err
	}
	sshDir := filepath.Dir(path)
	if err = os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	if err = os.Chown(sshDir, uid, gid); err != nil {
		return err
	}
	line, err := composeAuthorizedKeyLine(keyType, key, options, id, time.Now().Add(ttl))
	if err != nil {
		return err
	}
	return InjectFile(path, line, id, uid, gid)
}

// RevokeUser is the root-side revoke.
func RevokeUser(name, id string) error {
	path, uid, gid, err := AuthorizedKeysPath(name)
	if err != nil {
		return err
	}
	return RevokeFile(path, id, uid, gid)
}
