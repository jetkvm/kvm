package utils

import (
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"
)

type ByteSlice []byte

func (s ByteSlice) MarshalJSON() ([]byte, error) {
	vals := make([]int, len(s))
	for i, v := range s {
		vals[i] = int(v)
	}
	return json.Marshal(vals)
}

func (s *ByteSlice) UnmarshalJSON(data []byte) error {
	var vals []int
	if err := json.Unmarshal(data, &vals); err != nil {
		return err
	}
	*s = make([]byte, len(vals))
	for i, v := range vals {
		if v < 0 || v > 255 {
			return fmt.Errorf("value %d out of byte range", v)
		}
		(*s)[i] = byte(v)
	}
	return nil
}

func (k ByteSlice) MarshalZerologObject(e *zerolog.Event) {
	e.Hex("bytes", k)
}
