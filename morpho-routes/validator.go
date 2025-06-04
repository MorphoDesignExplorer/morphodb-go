package morphoroutes

import (
	"fmt"
	"reflect"
	"strings"
)

/*
Validates a struct with the validate struct tag (eg: `validate: "zero"`).

If the structure is invalid, an error is returned. Otherwise, nil.

Validation checks available:

zero: 	Check if the field is a zero value.

Example:

	type Record struct {
		Id 		int		`validate:"zero"`
		Name 	string
	}

Only the Id field in the above struct will be checked for a zero value.
*/
func Validate(value any) error {
	rt := reflect.TypeOf(value)
	rv := reflect.ValueOf(value)

	// TODO add more coverage for non struct and non slice types

	if rv.Kind() == reflect.Slice {
		if rv.IsNil() {
			return fmt.Errorf("%s is empty.", rt.Name())
		}
		return nil
	}

	emptyFields := make([]string, 0)

	for fieldId := range rt.NumField() {
		field := rt.Field(fieldId)
		if valType, ok := field.Tag.Lookup("validate"); ok && valType == "zero" {
			switch valType {
			case "zero":
				if rv.Field(fieldId).IsZero() {
					// check if field is empty here (which means it has a zero value)
					emptyFields = append(emptyFields, field.Name)
				}
			}
		}
	}

	if len(emptyFields) > 0 {
		return fmt.Errorf("the following fields were missing from %s: %s", rt.Name(), strings.Join(emptyFields, ", "))
	}

	return nil
}
