package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type restClient struct{}

type restClientInterface interface {
	Get(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error)
	Post(ctx context.Context, url string, headers map[string]string, body interface{}, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error)
	Patch(ctx context.Context, url string, headers map[string]string, body interface{}, timeout time.Duration) (statusCode int, responseBody []byte, err error)
	Delete(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (statusCode int, responseBody []byte, err error)
	PostFormData(ctx context.Context, reqUrl string, headers map[string]string, formData map[string]interface{}, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error)
}

var RestClient restClientInterface

func init() {
	RestClient = &restClient{}
}

func (rc *restClient) Get(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	request = request.WithContext(ctx)

	for key, value := range headers {
		request.Header.Add(key, value)
	}

	client := http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return
	}

	defer response.Body.Close()

	responseBody, err = io.ReadAll(response.Body)
	if err != nil {
		return
	}

	responseHeaders = make(map[string][]string)
	for key, values := range response.Header {
		responseHeaders[key] = values
	}

	statusCode = response.StatusCode

	return statusCode, responseBody, responseHeaders, nil
}

func (rc *restClient) Post(ctx context.Context, url string, headers map[string]string, body interface{}, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return
	}

	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return
	}
	request = request.WithContext(ctx)

	for key, value := range headers {
		request.Header.Add(key, value)
	}

	client := http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return
	}

	defer response.Body.Close()

	responseBody, err = io.ReadAll(response.Body)
	if err != nil {
		return
	}

	responseHeaders = make(map[string][]string)
	for key, values := range response.Header {
		responseHeaders[key] = values
	}

	statusCode = response.StatusCode

	return statusCode, responseBody, responseHeaders, nil
}

func (rc *restClient) Patch(ctx context.Context, url string, headers map[string]string, body interface{}, timeout time.Duration) (statusCode int, responseBody []byte, err error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return
	}

	request, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return
	}
	request = request.WithContext(ctx)

	for key, value := range headers {
		request.Header.Add(key, value)
	}

	client := http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return
	}

	defer response.Body.Close()

	responseBody, err = io.ReadAll(response.Body)
	if err != nil {
		return
	}

	statusCode = response.StatusCode

	return statusCode, responseBody, nil
}

func (rc *restClient) Delete(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (statusCode int, responseBody []byte, err error) {
	request, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return
	}

	for key, value := range headers {
		request.Header.Add(key, value)
	}

	client := http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	request = request.WithContext(ctx)

	defer response.Body.Close()

	responseBody, err = io.ReadAll(response.Body)
	if err != nil {
		return
	}

	statusCode = response.StatusCode

	return statusCode, responseBody, nil
}

func (rc *restClient) PostFormData(ctx context.Context, reqUrl string, headers map[string]string, formData map[string]interface{}, timeout time.Duration) (statusCode int, responseBody []byte, responseHeaders map[string][]string, err error) {
	var request = &http.Request{}

	values := url.Values{}
	for key, value := range formData {
		switch v := value.(type) {
		case string:
			values.Add(key, v)
		case int:
			values.Add(key, strconv.Itoa(v))
		case int64:
			values.Add(key, strconv.FormatInt(v, 10))
		case bool:
			values.Add(key, strconv.FormatBool(v))
		default:
			return 0, []byte{}, nil, errors.New("error While Adding Form data parameters to request url")
		}
	}

	request, err = http.NewRequest(http.MethodPost, reqUrl, bytes.NewBufferString(values.Encode()))

	if err != nil {
		return 0, []byte{}, nil, err
	}

	for key, value := range headers {
		request.Header.Add(key, value)
	}

	client := http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return 0, []byte{}, nil, err
	}

	defer response.Body.Close()

	responseBody, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return 0, []byte{}, nil, err
	}

	responseHeaders = make(map[string][]string)
	for key, values := range response.Header {
		responseHeaders[key] = values
	}

	statusCode = response.StatusCode

	return statusCode, responseBody, responseHeaders, nil
}
