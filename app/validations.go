package app

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

func validateIntType(fl validator.FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.Ptr:
		if field.Elem().Kind() == reflect.Int {
			return true
		} else if field.Elem().Kind() == reflect.String {
			// If the field is a pointer to string, try to parse it as int
			strVal := field.Elem().Interface().(string)
			_, err := strconv.Atoi(strings.TrimSpace(strVal))
			return err == nil

		}
	case reflect.Int:
		return true
	case reflect.String:
		_, err := strconv.Atoi(strings.TrimSpace(field.Interface().(string)))
		return err == nil
	}

	return false
}
