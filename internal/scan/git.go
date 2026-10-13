package scan

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if line, _, _ := strings.Cut(strings.TrimSpace(string(exit.Stderr)), "\n"); line != "" {
				return "", errors.New(line)
			}
		}
		return "", err
	}
	return string(out), nil
}
