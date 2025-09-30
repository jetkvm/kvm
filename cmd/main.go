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
	envChildID         = "JETKVM_CHILD_ID"
	errorDumpDir       = "/userdata/jetkvm/"
	errorDumpStateFile = ".has_error_dump"
	errorDumpTemplate  = "jetkvm-%s.log"
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

	cmd.Env = append(os.Environ(), []string{envChildID + "=" + kvm.GetBuiltAppVersion()}...)
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

func createErrorDump(logFile *os.File) {
	logFile.Close()

	// touch the error dump state file
	if err := os.WriteFile(filepath.Join(errorDumpDir, errorDumpStateFile), []byte{}, 0644); err != nil {
		return
	}

	fileName := fmt.Sprintf(errorDumpTemplate, time.Now().Format("20060102150405"))
	filePath := filepath.Join(errorDumpDir, fileName)
	if err := os.Rename(logFile.Name(), filePath); err == nil {
		fmt.Printf("error dump created: %s\n", filePath)
		return
	}

	fnSrc, err := os.Open(logFile.Name())
	if err != nil {
		return
	}
	defer fnSrc.Close()

	fnDst, err := os.Create(filePath)
	if err != nil {
		return
	}
	defer fnDst.Close()

	buf := make([]byte, 1024*1024)
	for {
		n, err := fnSrc.Read(buf)
		if err != nil && err != io.EOF {
			return
		}
		if n == 0 {
			break
		}

		if _, err := fnDst.Write(buf[:n]); err != nil {
			return
		}
	}

	fmt.Printf("error dump created: %s\n", filePath)
}

func doSupervise() {
	err := supervise()
	if err == nil {
		return
	}
}
