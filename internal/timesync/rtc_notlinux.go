//go:build !linux

package timesync

import (
	"errors"
	"time"
)

func (t *TimeSync) readRtcTime() (time.Time, error) {
	return time.Now(), nil
}

func (t *TimeSync) setRtcTime(tu time.Time) error {
	return errors.New("not supported")
}
