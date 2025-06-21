package usbgadget

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
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
	if bytes.Equal(oldContent, newContent) {
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

func (u *UsbGadget) logWithSupression(counterName string, every int, logger *zerolog.Logger, err error, msg string, args ...interface{}) {
	if _, ok := u.logSuppressionCounter[counterName]; !ok {
		u.logSuppressionCounter[counterName] = 0
	} else {
		u.logSuppressionCounter[counterName]++
	}

	l := logger.With().Int("counter", u.logSuppressionCounter[counterName]).Logger()

	if u.logSuppressionCounter[counterName]%every == 0 {
		if err != nil {
			l.Error().Err(err).Msgf(msg, args...)
		} else {
			l.Error().Msgf(msg, args...)
		}
	}
}

func (u *UsbGadget) resetLogSuppressionCounter(counterName string) {
	if _, ok := u.logSuppressionCounter[counterName]; !ok {
		u.logSuppressionCounter[counterName] = 0
	}
}
