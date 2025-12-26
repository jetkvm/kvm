package diagnostics

import (
	"os"
	"os/exec"
	"strings"
)

// runCmdLog runs a command and logs its output.
func (d *Diagnostics) runCmdLog(label string, cmd string, args ...string) {
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		d.logger.Warn().Str("cmd", label).Err(err).
			Str("output", strings.TrimSpace(string(output))).Msg("command failed")
		return
	}
	d.logger.Info().Str("output", strings.TrimSpace(string(output))).Msg(label)
}

// runShellLog runs a shell command (for pipelines) and logs its output.
func (d *Diagnostics) runShellLog(label, script string) {
	output, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil {
		d.logger.Warn().Str("cmd", label).Err(err).
			Str("output", strings.TrimSpace(string(output))).Msg("shell command failed")
		return
	}
	d.logger.Info().Str("output", strings.TrimSpace(string(output))).Msg(label)
}

// readFileLog reads a file and logs its content.
func (d *Diagnostics) readFileLog(label, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		d.logger.Warn().Str("path", path).Err(err).Msg(label + " NOT found")
		return
	}
	d.logger.Info().Str("content", strings.TrimSpace(string(data))).Msg(label)
}

// checkFileLog checks if a file/device exists and logs its status.
func (d *Diagnostics) checkFileLog(label, path string) {
	info, err := os.Stat(path)
	if err != nil {
		d.logger.Warn().Str("path", path).Err(err).Msg(label + " NOT found")
		return
	}
	d.logger.Info().Str("path", path).Str("mode", info.Mode().String()).Msg(label + " exists")
}

// listDirLog lists a directory and logs its contents.
func (d *Diagnostics) listDirLog(label, path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		d.logger.Warn().Str("path", path).Err(err).Msg(label + " NOT found")
		return
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	d.logger.Info().Strs("entries", names).Msg(label)
}
