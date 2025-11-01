package src

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type lockWaitHook func(wait time.Duration)

func acquireDirLock(ctx context.Context, path string, hook lockWaitHook) (func() error, error) {
	step := 120 * time.Millisecond
	waited := time.Duration(0)
	for {
		err := os.Mkdir(path, 0o755)
		if err == nil {
			meta := []byte(fmt.Sprintf("pid=%d\nacquired=%s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano)))
			_ = os.WriteFile(filepath.Join(path, "owner"), meta, 0o644)
			return func() error { return os.RemoveAll(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) && !os.IsExist(err) {
			return nil, err
		}

		if hook != nil {
			hook(waited)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(step):
			if waited < 2*time.Second {
				waited += step
			}
		}

		if waited >= 2*time.Second {
			if info, statErr := os.Stat(path); statErr == nil {
				if time.Since(info.ModTime()) > 10*time.Minute {
					_ = os.RemoveAll(path)
				}
			}
		}
	}
}
