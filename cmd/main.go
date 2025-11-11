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
)

const (
	envChildID        = "JETKVM_CHILD_ID"
	errorDumpDir      = "/userdata/jetkvm/crashdump"
	errorDumpLastFile = "last-crash.log"
	errorDumpTemplate = "jetkvm-%s.log"
)

func program() {
	gspt.SetProcTitle(os.Args[0] + " [app]")
	kvm.Main()
}

func main() {
	versionPtr := flag.Bool("version", false, "print version and exit")
	versionJSONPtr := flag.Bool("version-json", false, "print version as json and exit")
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

	childID := os.Getenv(envChildID)
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

	lastFilePath := filepath.Join(errorDumpDir, errorDumpLastFile)

	cmd.Env = append(os.Environ(), []string{
		fmt.Sprintf("%s=%s", envChildID, kvm.GetBuiltAppVersion()),
		fmt.Sprintf("JETKVM_LAST_ERROR_PATH=%s", lastFilePath),
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

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM)

		sig := <-sigChan
		_ = cmd.Process.Signal(sig)
	}()

	gspt.SetProcTitle(os.Args[0] + " [sup]")

	cmdErr := cmd.Wait()
	if cmdErr == nil {
		return nil
	}

	if exiterr, ok := cmdErr.(*exec.ExitError); ok {
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
	f, err := os.Stat(errorDumpDir)
	if err == nil && f.IsDir() {
		return nil
	}
	if err := os.MkdirAll(errorDumpDir, 0755); err != nil {
		return fmt.Errorf("failed to create error dump directory: %w", err)
	}
	return nil
}

func createErrorDump(logFile *os.File) {
	fmt.Println()

	fileName := fmt.Sprintf(
		errorDumpTemplate,
		time.Now().Format("20060102-150405"),
	)

	// check if the directory exists
	if err := ensureErrorDumpDir(); err != nil {
		fmt.Printf("failed to ensure error dump directory: %v\n", err)
		return
	}

	filePath := filepath.Join(errorDumpDir, fileName)
	if err := renameFile(logFile, filePath); err != nil {
		fmt.Printf("failed to rename file: %v\n", err)
		return
	}

	fmt.Printf("error dump copied: %s\n", filePath)

	lastFilePath := filepath.Join(errorDumpDir, errorDumpLastFile)

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
