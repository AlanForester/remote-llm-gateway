// Package secrets moves gateway credentials out of the initial process
// environment. /proc/self/environ keeps the environment from exec time, so
// unsetting a variable later does not hide it. The servers re-exec once with
// those variables removed, then load them back into the process only.
package secrets

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/internal/procenv"
)

const (
	cleanKey = "REMOTE_LLM_CLEAN"
	fileKey  = "REMOTE_LLM_SECRET_FILE"
)

// KeepProcClean re-execs the current process when its initial environment
// contains gateway credentials. A second start restores them for os.Getenv
// and deletes the temporary file.
func KeepProcClean() error {
	if os.Getenv(cleanKey) == "1" {
		return restore()
	}
	path, err := stash(os.Environ())
	if err != nil || path == "" {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	env := append(publicEnv(os.Environ()), cleanKey+"=1", fileKey+"="+path)
	if err := reexec(exe, os.Args, env); err != nil {
		_ = os.Remove(path)
		if errors.Is(err, errUnsupported) {
			return nil
		}
		return err
	}
	return nil
}

func stash(env []string) (string, error) {
	vals := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" || !procenv.Sensitive(key) {
			continue
		}
		vals[key] = value
	}
	if len(vals) == 0 {
		return "", nil
	}
	f, err := os.CreateTemp("", "remote-llm-")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		_ = os.Remove(path)
		return "", err
	}
	err = json.NewEncoder(f).Encode(vals)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(path)
		if err != nil {
			return "", err
		}
		return "", closeErr
	}
	return path, nil
}

func restore() error {
	path := os.Getenv(fileKey)
	_ = os.Unsetenv(fileKey)
	if path == "" {
		return errors.New("секретный файл не передан")
	}
	body, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return err
	}
	var vals map[string]string
	if err := json.Unmarshal(body, &vals); err != nil {
		return err
	}
	for key, value := range vals {
		if !procenv.Sensitive(key) {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

func publicEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == cleanKey || key == fileKey || procenv.Sensitive(key) {
			continue
		}
		out = append(out, entry)
	}
	return out
}
