package usbgadget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

func joinPath(basePath string, paths []string) string {
	pathArr := append([]string{basePath}, paths...)
	return filepath.Join(pathArr...)
}

func hexToDecimal(hex string) (int64, error) {
	decimal, err := strconv.ParseInt(hex, 16, 64)
	if err != nil {
		return 0, err
	}
	return decimal, nil
}

func decimalToOctal(decimal int64) string {
	return fmt.Sprintf("%04o", decimal)
}

func hexToOctal(hex string) (string, error) {
	hex = strings.ToLower(hex)
	hex = strings.Replace(hex, "0x", "", 1) //remove 0x or 0X

	decimal, err := hexToDecimal(hex)
	if err != nil {
		return "", err
	}

	// Convert the decimal integer to an octal string.
	octal := decimalToOctal(decimal)
	return octal, nil
}

func compareFileContent(oldContent []byte, newContent []byte, looserMatch bool) bool {
	if len(oldContent) == len(newContent) && bytes.Equal(oldContent, newContent) {
		return true
	}

	if len(oldContent) == len(newContent)+1 &&
		bytes.Equal(oldContent[:len(newContent)], newContent) &&
		oldContent[len(newContent)] == 10 {
		return true
	}

	if len(newContent) == 4 {
		if len(oldContent) < 6 || len(oldContent) > 7 {
			return false
		}

		if len(oldContent) == 7 && oldContent[6] == 0x0a {
			oldContent = oldContent[:6]
		}

		oldOctalValue, err := hexToOctal(string(oldContent))
		if err != nil {
			return false
		}

		if oldOctalValue == string(newContent) {
			return true
		}
	}

	if looserMatch {
		oldContentStr := strings.TrimSpace(string(oldContent))
		newContentStr := strings.TrimSpace(string(newContent))

		return oldContentStr == newContentStr
	}

	return false
}

func (u *UsbGadget) writeWithTimeout(file *os.File, data []byte) (n int, err error) {
	fileName := file.Name()

	if err := file.SetWriteDeadline(time.Now().Add(hidWriteTimeout)); err != nil {
		return -1, err
	}

	n, err = file.Write(data)
	if err == nil {
		u.resetLogSuppressionCounter("writeWithTimeout_" + fileName)
		return n, nil
	}

	context := u.getLoggingContext().Str("file", fileName).Bytes("data", data)
	_ = logging.LogTraceE(context, err, "write failed")

	if errors.Is(err, os.ErrDeadlineExceeded) {
		u.logWithSuppression(context, "writeWithTimeout_"+fileName, 10, err, "write timed out")
		// we've exceeded the suppression interval, so return an error so we can close and re-open the file
		// TODO return 0, err
		err = nil
	}

	return n, err
}
