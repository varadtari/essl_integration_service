package models

import "fmt"

type ApplicationError struct {
	Type      string                  `json:"type"`
	RequestId string                  `json:"requestId"`
	Message   ApplicationErrorMessage `json:"message"`
}
type ApplicationErrorMessage struct {
	ErrorCode          int         `json:"errorCode"`
	ErrorMessage       string      `json:"errorMessage"`
	DisplayMessage     string      `json:"displayMessage,omitempty"`
	OriginStatusCode   int         `json:"originStatusCode,omitempty"`
	OriginErrorMessage string      `json:"originErrorMessage,omitempty"`
	AdditionalInfo     interface{} `json:"additionalInfo,omitempty"`
}

type ApiError struct {
	StatusCode       int              `json:"statusCode"`
	ApplicationError ApplicationError `json:"applicationError"`
}

func (e *ApplicationError) String() string {
	return fmt.Sprintf("Type: %s, Message: %v", e.Type, e.Message)
}

// String returns a string representation of the ApiError.
func (e *ApiError) String() string {
	return fmt.Sprintf("StatusCode: %d, ApplicationError: %s", e.StatusCode, e.ApplicationError.Message.ErrorMessage)
}
