package udhcpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

func (p *DHCPClient) findUdhcpcProcess() (int, error) {
	// read procfs for udhcpc processes
	// we do not use procfs.AllProcs() because we want to avoid the overhead of reading the entire procfs
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

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

		cmdlineText := strings.Join(cmdline, " ")

		// check if it's a udhcpc process
		if strings.Contains(cmdlineText, fmt.Sprintf("-i %s", p.InterfaceName)) {
			p.logger.Debug().
				Str("pid", d.Name()).
				Interface("cmdline", cmdline).
				Msg("found udhcpc process")
			return pid, nil
		}
	}

	return 0, errors.New("udhcpc process not found")
}

func (c *DHCPClient) getProcessPid() (int, error) {
	var pid int
	if c.pidFile != "" {
		// try to read the pid file
		pidHandle, err := os.ReadFile(c.pidFile)
		if err != nil {
			c.logger.Warn().Err(err).
				Str("pidFile", c.pidFile).Msg("failed to read udhcpc pid file")
		}

		// if it exists, try to read the pid
		if pidHandle != nil {
			pidFromFile, err := strconv.Atoi(string(pidHandle))
			if err != nil {
				c.logger.Warn().Err(err).
					Str("pidFile", c.pidFile).Msg("failed to convert pid file to int")
			}
			pid = pidFromFile
		}
	}

	// if the pid is 0, try to find the pid using procfs
	if pid == 0 {
		newPid, err := c.findUdhcpcProcess()
		if err != nil {
			return 0, err
		}
		pid = newPid
	}

	return pid, nil
}

func (c *DHCPClient) getProcess() *os.Process {
	pid, err := c.getProcessPid()
	if err != nil {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		c.logger.Warn().Err(err).
			Int("pid", pid).Msg("failed to find process")
		return nil
	}

	return process
}

func (c *DHCPClient) GetProcess() *os.Process {
	if c.process == nil {
		process := c.getProcess()
		if process == nil {
			return nil
		}
		c.process = process
	}

	err := c.process.Signal(syscall.Signal(0))
	if err != nil && errors.Is(err, os.ErrProcessDone) {
		oldPid := c.process.Pid

		c.process = nil
		c.process = c.getProcess()
		if c.process == nil {
			c.logger.Error().Msg("failed to find new udhcpc process")
			return nil
		}
		c.logger.Warn().
			Int("oldPid", oldPid).
			Int("newPid", c.process.Pid).
			Msg("udhcpc process pid changed")
	} else if err != nil {
		c.logger.Warn().Err(err).
			Int("pid", c.process.Pid).Msg("udhcpc process is not running")
	}

	return c.process
}

func (c *DHCPClient) KillProcess() error {
	process := c.GetProcess()
	if process == nil {
		return nil
	}

	return process.Kill()
}

func (c *DHCPClient) ReleaseProcess() error {
	process := c.GetProcess()
	if process == nil {
		return nil
	}

	return process.Release()
}

func (c *DHCPClient) signalProcess(sig syscall.Signal) error {
	process := c.GetProcess()
	if process == nil {
		return nil
	}

	s := process.Signal(sig)
	if s != nil {
		c.logger.Warn().Err(s).
			Int("pid", process.Pid).
			Str("signal", sig.String()).
			Msg("failed to signal udhcpc process")
		return s
	}

	return nil
}

func (c *DHCPClient) Renew() error {
	return c.signalProcess(syscall.SIGUSR1)
}

func (c *DHCPClient) Release() error {
	return c.signalProcess(syscall.SIGUSR2)
}
