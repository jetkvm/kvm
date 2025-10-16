package jetdhcpc

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

type waitForCondition func(l netlink.Link, logger *zerolog.Logger) (ready bool, err error)

func (c *Client) waitFor(
	link netlink.Link,
	timeout <-chan time.Time,
	condition waitForCondition,
	timeoutError error,
) error {
	return waitFor(c.ctx, link, c.l, timeout, condition, timeoutError)
}

func waitFor(
	ctx context.Context,
	link netlink.Link,
	logger *zerolog.Logger,
	timeout <-chan time.Time,
	condition waitForCondition,
	timeoutError error,
) error {
	for {
		if ready, err := condition(link, logger); err != nil {
			return err
		} else if ready {
			break
		}

		select {
		case <-time.After(100 * time.Millisecond):
			continue
		case <-timeout:
			return timeoutError
		case <-ctx.Done():
			return timeoutError
		}
	}

	return nil
}
