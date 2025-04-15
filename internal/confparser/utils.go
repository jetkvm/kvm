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

func toString(v interface{}) (string, error) {
	switch v.(type) {
	case string:
		return v.(string), nil
	case null.String:
		return v.(null.String).String, nil
	}

	return "", fmt.Errorf("unsupported type: %s", reflect.TypeOf(v))
}
