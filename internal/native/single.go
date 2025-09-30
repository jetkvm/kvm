package native

import "sync"

var (
	instance     *Native
	instanceLock sync.RWMutex
)

func setInstance(n *Native) {
	instanceLock.Lock()
	defer instanceLock.Unlock()

	if instance == nil {
		instance = n
	}

	if instance != n {
		panic("instance is already set")
	}
}
