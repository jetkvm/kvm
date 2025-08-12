//go:build linux

package timesync

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func TimetoRtcTime(t time.Time) unix.RTCTime {
	return unix.RTCTime{
		Sec:   int32(t.Second()),
		Min:   int32(t.Minute()),
		Hour:  int32(t.Hour()),
		Mday:  int32(t.Day()),
		Mon:   int32(t.Month() - 1),
		Year:  int32(t.Year() - 1900),
		Wday:  int32(0),
		Yday:  int32(0),
		Isdst: int32(0),
	}
}

func RtcTimetoTime(t unix.RTCTime) time.Time {
	return time.Date(
		int(t.Year)+1900,
		time.Month(t.Mon+1),
		int(t.Mday),
		int(t.Hour),
		int(t.Min),
		int(t.Sec),
		0,
		time.UTC,
	)
}

func (t *TimeSync) getRtcDevice() (*os.File, error) {
	if t.rtcDevice == nil {
		file, err := os.OpenFile(t.rtcDevicePath, os.O_RDWR, 0666)
		if err != nil {
			return nil, err
		}
		t.rtcDevice = file
	}
	return t.rtcDevice, nil
}

func (t *TimeSync) getRtcDeviceFd() (int, error) {
	device, err := t.getRtcDevice()
	if err != nil {
		return 0, err
	}
	return int(device.Fd()), nil
}

// Read implements Read for the Linux RTC
func (t *TimeSync) readRtcTime() (time.Time, error) {
	fd, err := t.getRtcDeviceFd()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get RTC device fd: %w", err)
	}

	rtcTime, err := unix.IoctlGetRTCTime(fd)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get RTC time: %w", err)
	}

	date := RtcTimetoTime(*rtcTime)

	return date, nil
}

// Set implements Set for the Linux RTC
// ...
// It might be not accurate as the time consumed by the system call is not taken into account
// but it's good enough for our purposes
func (t *TimeSync) setRtcTime(tu time.Time) error {
	rt := TimetoRtcTime(tu)

	fd, err := t.getRtcDeviceFd()
	if err != nil {
		return fmt.Errorf("failed to get RTC device fd: %w", err)
	}

	currentRtcTime, err := t.readRtcTime()
	if err != nil {
		return fmt.Errorf("failed to read RTC time: %w", err)
	}

	t.l.Info().
		Interface("rtc_time", tu).
		Str("offset", tu.Sub(currentRtcTime).String()).
		Msg("set rtc time")

	if err := unix.IoctlSetRTCTime(fd, &rt); err != nil {
		return fmt.Errorf("failed to set RTC time: %w", err)
	}

	metricRTCUpdateCount.Inc()

	return nil
}
