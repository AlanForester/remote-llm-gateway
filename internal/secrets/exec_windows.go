//go:build windows

package secrets

func reexec(string, []string, []string) error {
	return errUnsupported
}
