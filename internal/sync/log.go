//go:build synctrace

package sync

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"

	"github.com/rs/zerolog"
)

var defaultLogger = logging.GetSubsystemLogger("synctrace")

func logTrace(msg string) {
	if defaultLogger.GetLevel() > zerolog.TraceLevel {
		return
	}

	logTrack(3).Trace().Msg(msg)
}

func logTrack(callerSkip int) *zerolog.Logger {
	l := *defaultLogger
	if l.GetLevel() > zerolog.TraceLevel {
		return &l
	}

	pc, file, no, ok := runtime.Caller(callerSkip)
	if ok {
		l = l.With().
			Str("file", file).
			Int("line", no).
			Logger()

		details := runtime.FuncForPC(pc)
		if details != nil {
			l = l.With().
				Str("func", details.Name()).
				Logger()
		}
	}

	return &l
}

func logLockTrack(ptr uintptr) *zerolog.Logger {
	l := logTrack(4).
		With().
		Str("ptr", fmt.Sprintf("%x", ptr)).
		Logger()
	return &l
}

type trackable interface {
	sync.Locker
}

type trackingEntry struct {
	lockCount   int
	unlockCount int
	firstLock   time.Time
	lastLock    time.Time
	lastUnlock  time.Time
}

var (
	indexMu  sync.Mutex
	tracking map[uintptr]*trackingEntry = make(map[uintptr]*trackingEntry)
)

func getPointer(t trackable) uintptr {
	return reflect.ValueOf(t).Pointer()
}

func increaseLockCount(ptr uintptr) {
	indexMu.Lock()
	defer indexMu.Unlock()

	entry, ok := tracking[ptr]
	if !ok {
		entry = &trackingEntry{}
		entry.firstLock = time.Now()
		tracking[ptr] = entry
	}
	entry.lockCount++
	entry.lastLock = time.Now()
}

func increaseUnlockCount(ptr uintptr) {
	indexMu.Lock()

	entry, ok := tracking[ptr]
	if !ok {
		entry = &trackingEntry{}
		tracking[ptr] = entry
	}
	entry.unlockCount++
	entry.lastUnlock = time.Now()
	delta := entry.lockCount - entry.unlockCount
	indexMu.Unlock()

	if !ok {
		logLockTrack(ptr).Warn().Interface("entry", entry).Msg("Unlock called without any prior Lock")
	} else if delta < 0 {
		logLockTrack(ptr).Warn().Interface("entry", entry).Msg("Unlock called more times than Lock")
	}
}

func logLock(t trackable) {
	ptr := getPointer(t)
	increaseLockCount(ptr)
	logLockTrack(ptr).Trace().Msg("locking mutex")
}

func logUnlock(t trackable) {
	ptr := getPointer(t)
	increaseUnlockCount(ptr)
	logLockTrack(ptr).Trace().Msg("unlocking mutex")
}

func logTryLock(t trackable) {
	ptr := getPointer(t)
	logLockTrack(ptr).Trace().Msg("trying to lock mutex")
}

func logTryLockResult(t trackable, l bool) {
	if !l {
		return
	}
	ptr := getPointer(t)
	increaseLockCount(ptr)
	logLockTrack(ptr).Trace().Msg("locked mutex")
}

func logRLock(t trackable) {
	ptr := getPointer(t)
	increaseLockCount(ptr)
	logLockTrack(ptr).Trace().Msg("locking mutex for reading")
}

func logRUnlock(t trackable) {
	ptr := getPointer(t)
	increaseUnlockCount(ptr)
	logLockTrack(ptr).Trace().Msg("unlocking mutex for reading")
}

func logTryRLock(t trackable) {
	ptr := getPointer(t)
	logLockTrack(ptr).Trace().Msg("trying to lock mutex for reading")
}

func logTryRLockResult(t trackable, l bool) {
	if !l {
		return
	}
	ptr := getPointer(t)
	increaseLockCount(ptr)
	logLockTrack(ptr).Trace().Msg("locked mutex for reading")
}

// You can call this function at any time to log any dangled locks currently tracked
// It is not an error for there to be open locks, but this can help identify any
// potential issues if called judiciously
func LogDangledLocks() {
	defaultLogger.Info().Msgf("Checking %v tracked locks for dangles", len(tracking))

	indexMu.Lock()
	var issues []struct {
		ptr   uintptr
		entry trackingEntry
	}
	for ptr, entry := range tracking {
		if entry.lockCount != entry.unlockCount {
			issues = append(issues, struct {
				ptr   uintptr
				entry trackingEntry
			}{ptr, *entry})
		}
	}
	indexMu.Unlock()

	defaultLogger.Info().Msgf("%v potential issues", len(issues))

	for _, issue := range issues {
		ptr := issue.ptr
		entry := issue.entry
		delta := entry.lockCount - entry.unlockCount

		failureType := "excess unlocks"
		if delta > 0 {
			failureType = "held locks"
		}

		defaultLogger.Warn().
			Str("ptr", fmt.Sprintf("%x", ptr)).
			Interface("entry", entry).
			Int("delta", delta).
			Msgf("dangled lock detected: %s", failureType)
	}
}
