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

func logLockTrack(i string) *zerolog.Logger {
	l := logTrack(4).
		With().
		Str("index", i).
		Logger()
	return &l
}

var (
	indexMu sync.Mutex

	lockCount   map[string]int       = make(map[string]int)
	unlockCount map[string]int       = make(map[string]int)
	lastLock    map[string]time.Time = make(map[string]time.Time)
)

type trackable interface {
	sync.Locker
}

func getIndex(t trackable) string {
	ptr := reflect.ValueOf(t).Pointer()
	return fmt.Sprintf("%x", ptr)
}

func increaseLockCount(i string) {
	indexMu.Lock()
	defer indexMu.Unlock()

	if _, ok := lockCount[i]; !ok {
		lockCount[i] = 0
	}
	lockCount[i]++

	if _, ok := lastLock[i]; !ok {
		lastLock[i] = time.Now()
	}
}

func increaseUnlockCount(i string) {
	indexMu.Lock()
	defer indexMu.Unlock()

	if _, ok := unlockCount[i]; !ok {
		unlockCount[i] = 0
	}
	unlockCount[i]++
}

func logLock(t trackable) {
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrack(i).Trace().Msg("locking mutex")
}

func logUnlock(t trackable) {
	i := getIndex(t)
	increaseUnlockCount(i)
	logLockTrack(i).Trace().Msg("unlocking mutex")
}

func logTryLock(t trackable) {
	i := getIndex(t)
	logLockTrack(i).Trace().Msg("trying to lock mutex")
}

func logTryLockResult(t trackable, l bool) {
	if !l {
		return
	}
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrack(i).Trace().Msg("locked mutex")
}

func logRLock(t trackable) {
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrack(i).Trace().Msg("locking mutex for reading")
}

func logRUnlock(t trackable) {
	i := getIndex(t)
	increaseUnlockCount(i)
	logLockTrack(i).Trace().Msg("unlocking mutex for reading")
}

func logTryRLock(t trackable) {
	i := getIndex(t)
	logLockTrack(i).Trace().Msg("trying to lock mutex for reading")
}

func logTryRLockResult(t trackable, l bool) {
	if !l {
		return
	}
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrack(i).Trace().Msg("locked mutex for reading")
}
