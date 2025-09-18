package usbgadget

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type ByteSlice []byte

func (s ByteSlice) MarshalJSON() ([]byte, error) {
	vals := make([]int, len(s))
	for i, v := range s {
		vals[i] = int(v)
	}
	return json.Marshal(vals)
}

func (s *ByteSlice) UnmarshalJSON(data []byte) error {
	var vals []int
	if err := json.Unmarshal(data, &vals); err != nil {
		return err
	}
	*s = make([]byte, len(vals))
	for i, v := range vals {
		if v < 0 || v > 255 {
			return fmt.Errorf("value %d out of byte range", v)
		}
		(*s)[i] = byte(v)
	}
	return nil
}

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

func (u *UsbGadget) writeWithTimeout(file *os.File, data []byte) (n int, err error) {
	if err := file.SetWriteDeadline(time.Now().Add(hidWriteTimeout)); err != nil {
		return -1, err
	}

	n, err = file.Write(data)
	if err == nil {
		return
	}

	u.log.Trace().
		Str("file", file.Name()).
		Bytes("data", data).
		Err(err).
		Msg("write failed")

	if errors.Is(err, os.ErrDeadlineExceeded) {
		u.logWithSuppression(
			fmt.Sprintf("writeWithTimeout_%s", file.Name()),
			1000,
			u.log,
			err,
			"write timed out: %s",
			file.Name(),
		)
		err = nil
	}

	return
}

func (u *UsbGadget) logWithSuppression(counterName string, every int, logger *zerolog.Logger, err error, msg string, args ...any) {
	u.logSuppressionLock.Lock()
	defer u.logSuppressionLock.Unlock()

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
	u.logSuppressionLock.Lock()
	defer u.logSuppressionLock.Unlock()

	if _, ok := u.logSuppressionCounter[counterName]; !ok {
		u.logSuppressionCounter[counterName] = 0
	}
}

func unlockWithLog(lock *sync.Mutex, logger *zerolog.Logger, msg string, args ...any) {
	logger.Trace().Msgf(msg, args...)
	lock.Unlock()
}
