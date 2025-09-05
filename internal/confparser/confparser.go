package confparser

import (
	"fmt"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/guregu/null/v6"
	"golang.org/x/net/idna"
)

type FieldConfig struct {
	Name              string
	Required          bool
	RequiredIf        map[string]any
	OneOf             []string
	ValidateTypes     []string
	Defaults          any
	IsEmpty           bool
	CurrentValue      any
	TypeString        string
	Delegated         bool
	shouldUpdateValue bool
}

func SetDefaultsAndValidate(config any) error {
	return setDefaultsAndValidate(config, true)
}

func setDefaultsAndValidate(config any, isRoot bool) error {
	// first we need to check if the config is a pointer
	if reflect.TypeOf(config).Kind() != reflect.Ptr {
		return fmt.Errorf("config is not a pointer")
	}

	// now iterate over the lease struct and set the values
	configType := reflect.TypeOf(config).Elem()
	configValue := reflect.ValueOf(config).Elem()

	fields := make(map[string]FieldConfig)

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)

		defaultValue := field.Tag.Get("default")

		fieldType := field.Type.String()

		fieldConfig := FieldConfig{
			Name:          field.Name,
			OneOf:         splitString(field.Tag.Get("one_of")),
			ValidateTypes: splitString(field.Tag.Get("validate_type")),
			RequiredIf:    make(map[string]any),
			CurrentValue:  fieldValue.Interface(),
			IsEmpty:       false,
			TypeString:    fieldType,
		}

		// check if the field is required
		required := field.Tag.Get("required")
		if required != "" {
			requiredBool, _ := strconv.ParseBool(required)
			fieldConfig.Required = requiredBool
		}

		var canUseOneOff = false

		// use switch to get the type
		switch fieldValue.Interface().(type) {
		case string, null.String:
			if defaultValue != "" {
				fieldConfig.Defaults = defaultValue
			}
			canUseOneOff = true
		case []string:
			if defaultValue != "" {
				fieldConfig.Defaults = strings.Split(defaultValue, ",")
			}
			canUseOneOff = true
		case int, null.Int:
			if defaultValue != "" {
				defaultValueInt, err := strconv.Atoi(defaultValue)
				if err != nil {
					return fmt.Errorf("invalid default value for field `%s`: %s", field.Name, defaultValue)
				}

				fieldConfig.Defaults = defaultValueInt
			}
		case bool, null.Bool:
			if defaultValue != "" {
				defaultValueBool, err := strconv.ParseBool(defaultValue)
				if err != nil {
					return fmt.Errorf("invalid default value for field `%s`: %s", field.Name, defaultValue)
				}

				fieldConfig.Defaults = defaultValueBool
			}
		default:
			if defaultValue != "" {
				return fmt.Errorf("field `%s` cannot use default value: unsupported type: %s", field.Name, fieldType)
			}

			// check if it's a pointer
			if fieldValue.Kind() == reflect.Ptr {
				// check if the pointer is nil
				if fieldValue.IsNil() {
					fieldConfig.IsEmpty = true
				} else {
					fieldConfig.CurrentValue = fieldValue.Elem().Addr()
					fieldConfig.Delegated = true
				}
			} else {
				fieldConfig.Delegated = true
			}
		}

		// now check if the field is nullable interface
		switch fieldValue.Interface().(type) {
		case null.String:
			if fieldValue.Interface().(null.String).IsZero() {
				fieldConfig.IsEmpty = true
			}
		case null.Int:
			if fieldValue.Interface().(null.Int).IsZero() {
				fieldConfig.IsEmpty = true
			}
		case null.Bool:
			if fieldValue.Interface().(null.Bool).IsZero() {
				fieldConfig.IsEmpty = true
			}
		case []string:
			if len(fieldValue.Interface().([]string)) == 0 {
				fieldConfig.IsEmpty = true
			}
		}

		// now check if the field has required_if
		requiredIf := field.Tag.Get("required_if")
		if requiredIf != "" {
			requiredIfParts := strings.SplitSeq(requiredIf, ",")
			for part := range requiredIfParts {
				partVal := strings.SplitN(part, "=", 2)
				if len(partVal) != 2 {
					return fmt.Errorf("invalid required_if for field `%s`: %s", field.Name, requiredIf)
				}

				fieldConfig.RequiredIf[partVal[0]] = partVal[1]
			}
		}

		// check if the field can use one_of
		if !canUseOneOff && len(fieldConfig.OneOf) > 0 {
			return fmt.Errorf("field `%s` cannot use one_of: unsupported type: %s", field.Name, fieldType)
		}

		fields[field.Name] = fieldConfig
	}

	if err := validateFields(config, fields); err != nil {
		return err
	}

	return nil
}

func validateFields(config any, fields map[string]FieldConfig) error {
	// now we can start to validate the fields
	for _, fieldConfig := range fields {
		if err := fieldConfig.validate(fields); err != nil {
			return err
		}

		fieldConfig.populate(config)
	}

	return nil
}

func (f *FieldConfig) validate(fields map[string]FieldConfig) error {
	var required bool
	var err error

	if required, err = f.validateRequired(fields); err != nil {
		return err
	}

	// check if the field needs to be updated and set defaults if needed
	if err := f.checkIfFieldNeedsUpdate(); err != nil {
		return err
	}

	// then we can check if the field is one_of
	if err := f.validateOneOf(); err != nil {
		return err
	}

	// and validate the type
	if err := f.validateField(); err != nil {
		return err
	}

	// if the field is delegated, we need to validate the nested field
	// but before that, let's check if the field is required
	if required && f.Delegated {
		if err := setDefaultsAndValidate(f.CurrentValue.(reflect.Value).Interface(), false); err != nil {
			return err
		}
	}

	return nil
}

func (f *FieldConfig) populate(config any) {
	// update the field if it's not empty
	if !f.shouldUpdateValue {
		return
	}

	reflect.ValueOf(config).Elem().FieldByName(f.Name).Set(reflect.ValueOf(f.CurrentValue))
}

func (f *FieldConfig) checkIfFieldNeedsUpdate() error {
	// populate the field if it's empty and has a default value
	if f.IsEmpty && f.Defaults != nil {
		switch f.CurrentValue.(type) {
		case null.String:
			f.CurrentValue = null.StringFrom(f.Defaults.(string))
		case null.Int:
			f.CurrentValue = null.IntFrom(int64(f.Defaults.(int)))
		case null.Bool:
			f.CurrentValue = null.BoolFrom(f.Defaults.(bool))
		case string:
			f.CurrentValue = f.Defaults.(string)
		case int:
			f.CurrentValue = f.Defaults.(int)
		case bool:
			f.CurrentValue = f.Defaults.(bool)
		case []string:
			f.CurrentValue = f.Defaults.([]string)
		default:
			return fmt.Errorf("field `%s` cannot use default value: unsupported type: %s", f.Name, f.TypeString)
		}

		f.shouldUpdateValue = true
	}

	return nil
}

func (f *FieldConfig) validateRequired(fields map[string]FieldConfig) (bool, error) {
	var required = f.Required

	// if the field is not required, we need to check if it's required_if
	if !required && len(f.RequiredIf) > 0 {
		for key, value := range f.RequiredIf {
			// check if the field's result matches the required_if
			// right now we only support string and int
			requiredField, ok := fields[key]
			if !ok {
				return required, fmt.Errorf("required_if field `%s` not found", key)
			}

			switch requiredField.CurrentValue.(type) {
			case string:
				if requiredField.CurrentValue.(string) == value.(string) {
					required = true
				}
			case int:
				if requiredField.CurrentValue.(int) == value.(int) {
					required = true
				}
			case null.String:
				if !requiredField.CurrentValue.(null.String).IsZero() &&
					requiredField.CurrentValue.(null.String).String == value.(string) {
					required = true
				}
			case null.Int:
				if !requiredField.CurrentValue.(null.Int).IsZero() &&
					requiredField.CurrentValue.(null.Int).Int64 == value.(int64) {
					required = true
				}
			}

			// if the field is required, we can break the loop
			// because we only need one of the required_if fields to be true
			if required {
				break
			}
		}
	}

	if required && f.IsEmpty {
		return false, fmt.Errorf("field `%s` is required", f.Name)
	}

	return required, nil
}

func checkIfSliceContains(slice []string, one_of []string) bool {
	for _, oneOf := range one_of {
		if slices.Contains(slice, oneOf) {
			return true
		}
	}

	return false
}

func (f *FieldConfig) validateOneOf() error {
	if len(f.OneOf) == 0 {
		return nil
	}

	var val []string
	switch f.CurrentValue.(type) {
	case string:
		val = []string{f.CurrentValue.(string)}
	case null.String:
		val = []string{f.CurrentValue.(null.String).String}
	case []string:
		// let's validate the value here
		val = f.CurrentValue.([]string)
	default:
		return fmt.Errorf("field `%s` cannot use one_of: unsupported type: %s", f.Name, f.TypeString)
	}

	if !checkIfSliceContains(val, f.OneOf) {
		return fmt.Errorf(
			"field `%s` is not one of the allowed values: %s, current value: %s",
			f.Name,
			strings.Join(f.OneOf, ", "),
			strings.Join(val, ", "),
		)
	}

	return nil
}

func (f *FieldConfig) validateField() error {
	if len(f.ValidateTypes) == 0 || f.IsEmpty {
		return nil
	}

	val, err := toString(f.CurrentValue)
	if err != nil {
		return fmt.Errorf("field `%s` cannot use validate_type: %s", f.Name, err)
	}

	if val == "" {
		return nil
	}

	for _, validateType := range f.ValidateTypes {
		switch validateType {
		case "ipv4":
			if net.ParseIP(val).To4() == nil {
				return fmt.Errorf("field `%s` is not a valid IPv4 address: %s", f.Name, val)
			}
		case "ipv6":
			if net.ParseIP(val).To16() == nil {
				return fmt.Errorf("field `%s` is not a valid IPv6 address: %s", f.Name, val)
			}
		case "hwaddr":
			if _, err := net.ParseMAC(val); err != nil {
				return fmt.Errorf("field `%s` is not a valid MAC address: %s", f.Name, val)
			}
		case "hostname":
			if _, err := idna.Lookup.ToASCII(val); err != nil {
				return fmt.Errorf("field `%s` is not a valid hostname: %s", f.Name, val)
			}
		case "proxy":
			if url, err := url.Parse(val); err != nil || (url.Scheme != "http" && url.Scheme != "https") || url.Host == "" {
				return fmt.Errorf("field `%s` is not a valid HTTP proxy URL: %s", f.Name, val)
			}
		default:
			return fmt.Errorf("field `%s` cannot use validate_type: unsupported validator: %s", f.Name, validateType)
		}
	}

	return nil
}
