package network

import "time"

func lifetimeToTime(lifetime int) *time.Time {
	if lifetime == 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(lifetime) * time.Second)
	return &t
}
