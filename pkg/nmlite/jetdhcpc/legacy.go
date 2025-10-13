package jetdhcpc

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
)

func readFileNoStat(filename string) ([]byte, error) {
	const maxBufferSize = 1024 * 1024

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := io.LimitReader(f, maxBufferSize)
	return io.ReadAll(reader)
}

func toCmdline(path string) ([]string, error) {
	data, err := readFileNoStat(path)
	if err != nil {
		return nil, err
	}

	if len(data) < 1 {
		return []string{}, nil
	}

	return strings.Split(string(bytes.TrimRight(data, "\x00")), "\x00"), nil
}

// KillUdhcpC kills all udhcpc processes
func KillUdhcpC(l *zerolog.Logger) error {
	// read procfs for udhcpc processes
	// we do not use procfs.AllProcs() because we want to avoid the overhead of reading the entire procfs
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	matchedPids := make([]int, 0)

	// iterate over the processes
	for _, d := range processes {
		// check if file is numeric
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}

		// check if it's a directory
		if !d.IsDir() {
			continue
		}

		cmdline, err := toCmdline(filepath.Join("/proc", d.Name(), "cmdline"))
		if err != nil {
			continue
		}

		if len(cmdline) < 1 {
			continue
		}

		if cmdline[0] != "udhcpc" {
			continue
		}

		matchedPids = append(matchedPids, pid)
	}

	if len(matchedPids) == 0 {
		l.Info().Msg("no udhcpc processes found")
		return nil
	}

	l.Info().Ints("pids", matchedPids).Msg("found udhcpc processes, terminating")

	for _, pid := range matchedPids {
		err := syscall.Kill(pid, syscall.SIGTERM)
		if err != nil {
			return err
		}

		l.Info().Int("pid", pid).Msg("terminated udhcpc process")
	}

	return nil
}

func (c *Client) killUdhcpc() error {
	return KillUdhcpC(c.l)
}
