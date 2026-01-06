package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/erikdubbelboer/gspt"
	"github.com/jetkvm/kvm"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/supervisor"
)

var (
	subcomponent string
)

func program() {
	subcomponentOverride := os.Getenv(supervisor.EnvSubcomponent)
	if subcomponentOverride != "" {
		subcomponent = subcomponentOverride
	}
	switch subcomponent {
	case "native":
		native.RunNativeProcess(os.Args[0])
	default:
		kvm.Main()
	}
}

func setProcTitle(status string) {
	if status != "" {
		status = " " + status
	}
	title := fmt.Sprintf("jetkvm: [supervisor]%s", status)
	gspt.SetProcTitle(title)
}

func main() {
	versionPtr := flag.Bool("version", false, "print version and exit")
	versionJSONPtr := flag.Bool("version-json", false, "print version as json and exit")
	flag.StringVar(&subcomponent, "subcomponent", "", "subcomponent to run")
	flag.Parse()

	if *versionPtr || *versionJSONPtr {
		versionData, err := kvm.GetVersionData(*versionJSONPtr)
		if err != nil {
			fmt.Printf("failed to get version data: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(versionData))
		return
	}

	childID := os.Getenv(supervisor.EnvChildID)
	switch childID {
	case "":
		doSupervise()
	case kvm.GetBuiltAppVersion():
		program()
	default:
		fmt.Printf("Invalid build version: %s != %s\n", childID, kvm.GetBuiltAppVersion())
		os.Exit(1)
	}
}

func supervise() error {
	setProcTitle("")

	// check binary path
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// check if binary is same as current binary
	if info, statErr := os.Stat(binPath); statErr != nil {
		return fmt.Errorf("failed to get executable info: %w", statErr)
		// check if binary is empty
	} else if info.Size() == 0 {
		return fmt.Errorf("binary is empty")
		// check if it's executable
	} else if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("binary is not executable")
	}
	// run the child binary
	cmd := exec.Command(binPath)

	lastFilePath := filepath.Join(supervisor.ErrorDumpDir, supervisor.ErrorDumpLastFile)

	cmd.Env = append(os.Environ(), []string{
		fmt.Sprintf("%s=%s", supervisor.EnvChildID, kvm.GetBuiltAppVersion()),
		fmt.Sprintf("%s=%s", supervisor.ErrorDumpLastFile, lastFilePath),
	}...)
	cmd.Args = os.Args

	logFile, err := os.CreateTemp("", "jetkvm-stdout.log")
	defer func() {
		// we don't care about the errors here
		_ = logFile.Close()
		_ = os.Remove(logFile.Name())
	}()
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Use io.MultiWriter to write to both the original streams and our buffers
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("failed to start command: %w", startErr)
	}

	setProcTitle(fmt.Sprintf("started (pid=%d)", cmd.Process.Pid))

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM)

		sig := <-sigChan
		_ = cmd.Process.Signal(sig)
	}()

	cmdErr := cmd.Wait()
	if cmdErr == nil {
		return nil
	}

	if exiterr, ok := cmdErr.(*exec.ExitError); ok {
		// Append crash info to the log file
		fmt.Fprintf(logFile, "\n=== SUPERVISOR CRASH INFO ===\n")
		fmt.Fprintf(logFile, "Timestamp: %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Exit Code: %d\n", exiterr.ExitCode())
		if ws, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				fmt.Fprintf(logFile, "Signal: %s\n", ws.Signal())
			}
		}

		createErrorDump(logFile)
		os.Exit(exiterr.ExitCode())
	}

	return nil
}

func isSymlinkTo(oldName, newName string) bool {
	file, err := os.Stat(newName)
	if err != nil {
		return false
	}
	if file.Mode()&os.ModeSymlink != os.ModeSymlink {
		return false
	}
	target, err := os.Readlink(newName)
	if err != nil {
		return false
	}
	return target == oldName
}

func ensureSymlink(oldName, newName string) error {
	if isSymlinkTo(oldName, newName) {
		return nil
	}
	_ = os.Remove(newName)
	return os.Symlink(oldName, newName)
}

func renameFile(f *os.File, newName string) error {
	_ = f.Close()

	// try to rename the file first
	if err := os.Rename(f.Name(), newName); err == nil {
		return nil
	}

	// copy the log file to the error dump directory
	fnSrc, err := os.Open(f.Name())
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer fnSrc.Close()

	fnDst, err := os.Create(newName)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer fnDst.Close()

	buf := make([]byte, 1024*1024)
	for {
		n, err := fnSrc.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file: %w", err)
		}
		if n == 0 {
			break
		}

		if _, err := fnDst.Write(buf[:n]); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
	}

	return nil
}

func ensureErrorDumpDir() error {
	// TODO: check if the directory is writable
	f, err := os.Stat(supervisor.ErrorDumpDir)
	if err == nil && f.IsDir() {
		return nil
	}
	if err := os.MkdirAll(supervisor.ErrorDumpDir, 0755); err != nil {
		return fmt.Errorf("failed to create error dump directory: %w", err)
	}
	return nil
}

func createErrorDump(logFile *os.File) {
	fmt.Println()

	fileName := fmt.Sprintf(
		supervisor.ErrorDumpTemplate,
		time.Now().Format("20060102-150405"),
	)

	// check if the directory exists
	if err := ensureErrorDumpDir(); err != nil {
		fmt.Printf("failed to ensure error dump directory: %v\n", err)
		return
	}

	filePath := filepath.Join(supervisor.ErrorDumpDir, fileName)
	if err := renameFile(logFile, filePath); err != nil {
		fmt.Printf("failed to rename file: %v\n", err)
		return
	}

	fmt.Printf("error dump copied: %s\n", filePath)

	lastFilePath := filepath.Join(supervisor.ErrorDumpDir, supervisor.ErrorDumpLastFile)

	if err := ensureSymlink(filePath, lastFilePath); err != nil {
		fmt.Printf("failed to create symlink: %v\n", err)
		return
	}
}

func doSupervise() {
	err := supervise()
	if err == nil {
		return
	}
}
