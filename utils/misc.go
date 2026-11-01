package utils

import (
	"encoding/json"
	"fmt"
	"os"

	"essl_integration_service/logger"

	_ "essl_integration_service/env"
)

func ShowServiceInfo() {

	serviceName := os.Getenv("SERVICE_NAME")
	fmt.Printf("Starting service %s!\n", serviceName)

	envName := os.Getenv("ENV")
	fmt.Printf("Environment is %s!\n", envName)

}

func ConvertStructToString(structInput interface{}) string {

	structInputByteArray, err := json.Marshal(structInput)
	if err != nil {
		errorMsg := fmt.Sprintf("Could not convert struct input to byte array.Struct value:%v.Error message:%s!", structInput, err.Error())
		logger.Log.Error(errorMsg)
		return ""
	}

	return string(structInputByteArray)

}

// Ptr returns a pointer to v, for populating optional struct fields inline.
func Ptr[T any](v T) *T {
	return &v
}
