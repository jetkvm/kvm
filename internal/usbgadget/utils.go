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

func compareIgnoreTrailingLF(shorter []byte, longer []byte) bool {
	shorterLen := len(shorter)
	longerLen := len(longer)
	return shorterLen+1 == longerLen &&
		bytes.Equal(longer[:shorterLen], shorter) &&
		longer[shorterLen] == 0x0a
}

func compareFileContent(oldContent []byte, newContent []byte, looserMatch bool) bool {
	if len(oldContent) == len(newContent) && bytes.Equal(oldContent, newContent) {
		return true
	}

	// allow for a trailing newline difference if the one did have one and the other does NOT
	if compareIgnoreTrailingLF(oldContent, newContent) || compareIgnoreTrailingLF(newContent, oldContent) {
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

	logger := u.log.With().Str("file", fileName).Bytes("data", data).Logger()
	logger.Trace().Err(err).Msg("write failed")

	if errors.Is(err, os.ErrClosed) {
		logger.Warn().Msg("file is closed, stopping writes")
		return 0, err
	} else if errors.Is(err, os.ErrDeadlineExceeded) {
		if exceeded := u.logWithSuppression("writeWithTimeout_"+fileName, 10, &logger, err, "write timed out"); exceeded {
			logger.Error().Msg("too many errors writing to the file, stopping writes")
			return 0, err
		}
		return 0, nil
	}

	return n, err
}
