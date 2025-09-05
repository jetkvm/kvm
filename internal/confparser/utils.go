package confparser

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/guregu/null/v6"
)

func splitString(s string) []string {
	if s == "" {
		return []string{}
	}

	return strings.Split(s, ",")
}

func toString(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case null.String:
		return v.String, nil
	}

	return "", fmt.Errorf("unsupported type: %s", reflect.TypeOf(v))
}
