package udhcpc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

const (
	DHCPLeaseFile = "/run/udhcpc.%s.info"
	DHCPPidFile   = "/run/udhcpc.%s.pid"
)

type DHCPClient struct {
	InterfaceName string
	leaseFile     string
	pidFile       string
	lease         *Lease
	logger        *zerolog.Logger
	process       *os.Process
	onLeaseChange func(lease *Lease)
}

type DHCPClientOptions struct {
	InterfaceName string
	PidFile       string
	Logger        *zerolog.Logger
	OnLeaseChange func(lease *Lease)
}

var defaultLogger = zerolog.New(os.Stdout).Level(zerolog.InfoLevel)

func NewDHCPClient(options *DHCPClientOptions) *DHCPClient {
	if options.Logger == nil {
		options.Logger = &defaultLogger
	}

	l := options.Logger.With().Str("interface", options.InterfaceName).Logger()
	return &DHCPClient{
		InterfaceName: options.InterfaceName,
		logger:        &l,
		leaseFile:     fmt.Sprintf(DHCPLeaseFile, options.InterfaceName),
		pidFile:       options.PidFile,
		onLeaseChange: options.OnLeaseChange,
	}
}

func (c *DHCPClient) getWatchPaths() []string {
	watchPaths := make(map[string]interface{})
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
// this isn't a blocking call, and the lease file is reloaded when a change is detected.
func (c *DHCPClient) Run() error {
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
					c.logger.Debug().
						Str("event", event.Op.String()).
						Str("path", event.Name).
						Msg("udhcpc lease file updated, reloading lease")
					_ = c.loadLeaseFile()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				c.logger.Error().Err(err).Msg("error watching lease file")
			}
		}
	}()

	for _, path := range c.getWatchPaths() {
		err = watcher.Add(path)
		if err != nil {
			c.logger.Error().
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

	// block the goroutine until the lease file is updated
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
		c.logger.Debug().Msg("udhcpc lease file is empty")
		return nil
	}

	lease := &Lease{}
	err = UnmarshalDHCPCLease(lease, string(file))
	if err != nil {
		return err
	}

	isFirstLoad := c.lease == nil
	c.lease = lease

	if lease.IPAddress == nil {
		c.logger.Info().
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
		c.logger.Error().Err(err).Msg("failed to get dhcp lease expiry")
	} else {
		expiresIn := time.Until(leaseExpiry)
		c.logger.Info().
			Interface("expiry", leaseExpiry).
			Str("expiresIn", expiresIn.String()).
			Msg("current dhcp lease expiry time calculated")
	}

	c.onLeaseChange(lease)

	c.logger.Info().
		Str("ip", lease.IPAddress.String()).
		Str("leaseTime", lease.LeaseTime.String()).
		Interface("data", lease).
		Msg(msg)

	return nil
}

func (c *DHCPClient) GetLease() *Lease {
	return c.lease
}
