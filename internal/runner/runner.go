package runner

import (
	"errors"

	"ktbs.dev/mubeng/common"
	"ktbs.dev/mubeng/internal/checker"
	"ktbs.dev/mubeng/internal/daemon"
	"ktbs.dev/mubeng/internal/downloader"
	"ktbs.dev/mubeng/internal/server"
)

// New to switch an action, whether to check or run a proxy server.
func New(opt *common.Options) error {
	switch {
	case opt.Address != "":
		if opt.Daemon {
			return daemon.New(opt)
		}

		server.Run(opt)
	case opt.Check:
		checker.Do(opt)

		if opt.Output != "" {
			defer opt.Result.Close()
		}
	case opt.Download:
		return downloader.Do(opt)
	default:
		return errors.New("no action to run")
	}

	return nil
}
