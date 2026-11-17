package udhcpc

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/rs/zerolog"
)

func KillAll(l *zerolog.Logger) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	for _, d := range entries {
		pid, err := strconv.Atoi(d.Name())
		if err != nil || !d.IsDir() {
			continue
		}

		cmdline, err := toCmdline(filepath.Join("/proc", d.Name(), "cmdline"))
		if err != nil || len(cmdline) < 1 || cmdline[0] != "udhcpc" {
			continue
		}

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}
		l.Info().Int("pid", pid).Msg("terminated udhcpc process")
	}

	return nil
}
