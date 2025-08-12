package network

import (
	"encoding/json"
	"time"
)

func lifetimeToTime(lifetime int) *time.Time {
	if lifetime == 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(lifetime) * time.Second)
	return &t
}

func IsSame(a, b interface{}) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}
