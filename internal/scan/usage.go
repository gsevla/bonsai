package scan

import (
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func usage(root string) (int64, time.Time) {
	var total int64
	var latest time.Time
	now := time.Now()
	links := map[[2]uint64]struct{}{}
	gitDir := filepath.Join(root, ".git") + string(filepath.Separator)
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		if !entry.IsDir() && stat.Nlink > 1 {
			key := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
			if _, counted := links[key]; counted {
				return nil
			}
			links[key] = struct{}{}
		}
		total += int64(stat.Blocks) * 512
		modified := info.ModTime()
		if info.Mode().IsRegular() && !strings.HasPrefix(path, gitDir) && modified.After(latest) && !modified.After(now) {
			latest = modified
		}
		return nil
	})
	return total, latest
}
