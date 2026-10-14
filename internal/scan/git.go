package scan

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
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

const fetchTimeout = 60 * time.Second

func Fetch(common string) bool {
	remotes, err := git(common, "remote")
	if err != nil {
		return false
	}
	if strings.TrimSpace(remotes) == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", common, "fetch", "--all", "--prune", "--quiet")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "LC_ALL=C")
	return cmd.Run() == nil
}
