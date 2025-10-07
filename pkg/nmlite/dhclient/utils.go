package dhclient

import (
	"context"
	"time"

	"github.com/vishvananda/netlink"
)

type waitForCondition func(l netlink.Link) (ready bool, err error)

func (c *Client) waitFor(
	link netlink.Link,
	timeout <-chan time.Time,
	condition waitForCondition,
	timeoutError error,
) error {
	return waitFor(c.ctx, link, timeout, condition, timeoutError)
}

func waitFor(
	ctx context.Context,
	link netlink.Link,
	timeout <-chan time.Time,
	condition waitForCondition,
	timeoutError error,
) error {
	for {
		if ready, err := condition(link); err != nil {
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
