package models

import (
	"encoding/json"
	"fmt"
)

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

// MarshalJSON keeps internal detail out of API responses. errorMessage carries raw database and
// upstream errors (hosts, SQL, constraint names) and additionalInfo carries internal values, so
// both are only sent when EXPOSE_ERROR_DETAILS is on for local development. Logs and
// internal_errors always keep the full message, joined to the response by its requestId.
func (m ApplicationErrorMessage) MarshalJSON() ([]byte, error) {

	type wire ApplicationErrorMessage

	if EXPOSE_ERROR_DETAILS {
		return json.Marshal(wire(m))
	}

	type public struct {
		ErrorCode        int    `json:"errorCode"`
		DisplayMessage   string `json:"displayMessage,omitempty"`
		OriginStatusCode int    `json:"originStatusCode,omitempty"`
	}

	return json.Marshal(public{
		ErrorCode:        m.ErrorCode,
		DisplayMessage:   m.DisplayMessage,
		OriginStatusCode: m.OriginStatusCode,
	})
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
