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

func getLogger() *zerolog.Logger {
	return logging.GetSubsystemLogger("synctrace")
}

func logTrack(callerSkip int) *zerolog.Event {
	logger := getLogger()
	if !logging.IsTraceLevel(logger) {
		return logger.Trace()
	}

	traceEvent := logger.Trace()

	pc, file, no, ok := runtime.Caller(callerSkip)
	if ok {
		traceEvent = traceEvent.Str("file", file).Int("line", no)

		details := runtime.FuncForPC(pc)
		if details != nil {
			traceEvent = traceEvent.Str("func", details.Name())
		}
	}

	return traceEvent
}

func logTrace(msg string) {
	logTrack(3).Msg(msg)
}

func logLockTrace(i string) *zerolog.Event {
	return logTrack(4).Str("index", i)
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
	logLockTrace(i).Msg("locking mutex")
}

func logUnlock(t trackable) {
	i := getIndex(t)
	increaseUnlockCount(i)
	logLockTrace(i).Msg("unlocking mutex")
}

func logTryLock(t trackable) {
	i := getIndex(t)
	logLockTrace(i).Msg("trying to lock mutex")
}

func logTryLockResult(t trackable, l bool) {
	if !l {
		return
	}
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrace(i).Msg("locked mutex")
}

func logRLock(t trackable) {
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrace(i).Msg("locking mutex for reading")
}

func logRUnlock(t trackable) {
	i := getIndex(t)
	increaseUnlockCount(i)
	logLockTrace(i).Msg("unlocking mutex for reading")
}

func logTryRLock(t trackable) {
	i := getIndex(t)
	logLockTrace(i).Msg("trying to lock mutex for reading")
}

func logTryRLockResult(t trackable, l bool) {
	if !l {
		return
	}
	i := getIndex(t)
	increaseLockCount(i)
	logLockTrace(i).Msg("locked mutex for reading")
}
