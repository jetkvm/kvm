package udhcpc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/sync"
	"github.com/rs/zerolog"

	"github.com/fsnotify/fsnotify"
	"github.com/jetkvm/kvm/internal/network/types"
)

const (
	DHCPLeaseFile = "/run/udhcpc.%s.info"
	DHCPPidFile   = "/run/udhcpc.%s.pid"
)

type DHCPClient struct {
	types.DHCPClient
	InterfaceName string
	leaseFile     string
	pidFile       string
	lease         *Lease
	process       *os.Process
	runOnce       sync.Once
	onLeaseChange func(lease *types.DHCPLease)
}

type DHCPClientOptions struct {
	InterfaceName string
	PidFile       string
	OnLeaseChange func(lease *types.DHCPLease)
}

func NewDHCPClient(options *DHCPClientOptions) *DHCPClient {
	return &DHCPClient{
		InterfaceName: options.InterfaceName,
		leaseFile:     fmt.Sprintf(DHCPLeaseFile, options.InterfaceName),
		pidFile:       options.PidFile,
		onLeaseChange: options.OnLeaseChange,
	}
}

func (c *DHCPClient) getLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("nmlite").
		With().
		Str("subcomponent", "udhcpc").
		Str("interface", c.InterfaceName).
		Str("pidFile", c.pidFile).
		Str("leaseFile", c.leaseFile).
		Logger()
	return &logger
}

func (c *DHCPClient) getWatchPaths() []string {
	watchPaths := make(map[string]any)
	watchPaths[filepath.Dir(c.leaseFile)] = nil

	if c.pidFile != "" {
		watchPaths[filepath.Dir(c.pidFile)] = nil
	}

	paths := make([]string, 0)
	for path := range watchPaths {
		paths = append(paths, path)
	}
	return paths
}

// Run starts the DHCP client and watches the lease file for changes.
// this is a blocking call.
func (c *DHCPClient) run() error {
	err := c.loadLeaseFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					continue
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
					continue
				}

				if event.Name == c.leaseFile {
					c.getLogger().Debug().
						Str("event", event.Op.String()).
						Str("path", event.Name).
						Msg("udhcpc lease file updated, reloading lease")
					_ = c.loadLeaseFile()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				c.getLogger().Error().Err(err).Msg("error watching lease file")
			}
		}
	}()

	for _, path := range c.getWatchPaths() {
		err = watcher.Add(path)
		if err != nil {
			c.getLogger().Error().
				Err(err).
				Str("path", path).
				Msg("failed to watch directory")
			return err
		}
	}

	// TODO: update udhcpc pid file
	// we'll comment this out for now because the pid might change
	// process := c.GetProcess()
	// if process == nil {
	// 	c.logger.Error().Msg("udhcpc process not found")
	// }

	// block the goroutine
	<-make(chan struct{})

	return nil
}

func (c *DHCPClient) loadLeaseFile() error {
	file, err := os.ReadFile(c.leaseFile)
	if err != nil {
		return err
	}

	data := string(file)
	if data == "" {
		c.getLogger().Debug().Msg("udhcpc lease file is empty")
		return nil
	}

	lease := &Lease{}
	err = UnmarshalDHCPCLease(lease, string(file))
	if err != nil {
		return err
	}

	isFirstLoad := c.lease == nil

	// Skip processing if lease hasn't changed to avoid unnecessary wake-ups.
	if reflect.DeepEqual(c.lease, lease) {
		return nil
	}

	c.lease = lease

	if lease.IPAddress == nil {
		c.getLogger().Info().
			Interface("lease", lease).
			Str("data", string(file)).
			Msg("udhcpc lease cleared")
		return nil
	}

	msg := "udhcpc lease updated"
	if isFirstLoad {
		msg = "udhcpc lease loaded"
	}

	leaseExpiry, err := lease.SetLeaseExpiry()
	if err != nil {
		c.getLogger().Error().Err(err).Msg("failed to get dhcp lease expiry")
	} else {
		expiresIn := time.Until(leaseExpiry)
		c.getLogger().Info().
			Interface("expiry", leaseExpiry).
			Dur("expiresIn", expiresIn).
			Msg("current dhcp lease expiry time calculated")
	}

	c.onLeaseChange(lease.ToDHCPLease())

	c.getLogger().Info().
		IPAddr("ip", lease.IPAddress).
		Dur("leaseTime", lease.LeaseTime).
		Interface("data", lease).
		Msg(msg)

	return nil
}

func (c *DHCPClient) GetLease() *Lease {
	return c.lease
}

func (c *DHCPClient) Domain() string {
	return c.lease.Domain
}

func (c *DHCPClient) Lease4() *types.DHCPLease {
	if c.lease == nil {
		return nil
	}
	return c.lease.ToDHCPLease()
}

func (c *DHCPClient) Lease6() *types.DHCPLease {
	// TODO: implement
	return nil
}

func (c *DHCPClient) SetIPv4(enabled bool) {
	// TODO: implement
}

func (c *DHCPClient) SetIPv6(enabled bool) {
	// TODO: implement
}

func (c *DHCPClient) SetOnLeaseChange(callback func(lease *types.DHCPLease)) {
	c.onLeaseChange = callback
}

func (c *DHCPClient) Start() error {
	c.runOnce.Do(func() {
		go func() {
			err := c.run()
			if err != nil {
				c.getLogger().Error().Err(err).Msg("failed to run udhcpc")
			}
		}()
	})
	return nil
}

func (c *DHCPClient) Stop() error {
	return c.KillProcess() // udhcpc already has KillProcess()
}
