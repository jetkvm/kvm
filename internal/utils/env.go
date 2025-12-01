package utils

import (
	"fmt"
	"reflect"
	"strconv"
)

func MarshalEnv(instance interface{}) ([]string, error) {
	v := reflect.ValueOf(instance)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, fmt.Errorf("instance is nil")
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("instance must be a struct or pointer to struct")
	}

	t := v.Type()
	var result []string

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Get the env tag
		envTag := field.Tag.Get("env")
		if envTag == "" || envTag == "-" {
			continue
		}

		// Skip unexported fields
		if !fieldValue.CanInterface() {
			continue
		}

		var valueStr string

		// Handle different types
		switch fieldValue.Kind() {
		case reflect.Bool:
			valueStr = strconv.FormatBool(fieldValue.Bool())

		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			valueStr = strconv.FormatUint(fieldValue.Uint(), 10)

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			valueStr = strconv.FormatInt(fieldValue.Int(), 10)

		case reflect.Float32, reflect.Float64:
			valueStr = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)

		case reflect.String:
			valueStr = fieldValue.String()

		case reflect.Ptr:
			if fieldValue.IsNil() {
				continue // Skip nil pointers
			}
			elem := fieldValue.Elem()
			// Handle *semver.Version and other pointer types
			if elem.CanInterface() {
				if stringer, ok := elem.Interface().(fmt.Stringer); ok {
					valueStr = stringer.String()
				} else {
					valueStr = fmt.Sprintf("%v", elem.Interface())
				}
			} else {
				valueStr = fmt.Sprintf("%v", elem.Interface())
			}

		default:
			// For other types, try to convert to string
			if fieldValue.CanInterface() {
				if stringer, ok := fieldValue.Interface().(fmt.Stringer); ok {
					valueStr = stringer.String()
				} else {
					valueStr = fmt.Sprintf("%v", fieldValue.Interface())
				}
			} else {
				valueStr = fmt.Sprintf("%v", fieldValue.Interface())
			}
		}

		result = append(result, fmt.Sprintf("%s=%s", envTag, valueStr))
	}

	return result, nil
}
