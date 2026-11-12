package xhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	net_url "net/url"
	"time"
)

const (
	defaultReadLimitBytes                = 4 * 1024 * 1024
	defaultTimeoutDuration time.Duration = 5 * time.Second
)

type JSONClient struct {
	timeoutDuration time.Duration
	readLimitBytes  int64
}

func NewJSONClient(opts ...Option) *JSONClient {
	j := JSONClient{}
	for _, opt := range opts {
		if opt != nil {
			opt(&j)
		}
	}

	repair(&j)
	return &j
}

func (j *JSONClient) Get(ctx context.Context, url string, header map[string]string, params map[string]string, resp any) error {
	return j.Do(ctx, http.MethodGet, getCompleteURL(url, params), header, nil, resp)
}

func (j *JSONClient) Post(ctx context.Context, url string, header map[string]string, req, resp any) error {
	return j.Do(ctx, http.MethodPost, url, header, req, resp)
}

func (j *JSONClient) Patch(ctx context.Context, url string, header map[string]string, req, resp any) error {
	return j.Do(ctx, http.MethodPatch, url, header, req, resp)
}

func (j *JSONClient) Delete(ctx context.Context, url string, header map[string]string, req, resp any) error {
	return j.Do(ctx, http.MethodDelete, url, header, req, resp)
}

func (j *JSONClient) Do(ctx context.Context, method string, url string, header map[string]string, req, resp any) error {
	if j == nil {
		return fmt.Errorf("nil http client")
	}
	if ctx == nil {
		return fmt.Errorf("nil http context")
	}
	if method == "" || url == "" {
		return fmt.Errorf("http method and url are required")
	}
	tCtx, cancel := context.WithTimeout(ctx, j.timeoutDuration)
	defer cancel()

	var reqBody []byte
	var err error
	if req != nil {
		reqBody, err = json.Marshal(req)
		if err != nil {
			return err
		}
	}

	request, err := http.NewRequestWithContext(tCtx, method, url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	for k, v := range header {
		request.Header.Set(k, v)
	}
	if req != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(response.Body, j.readLimitBytes+1))
	if err != nil {
		return err
	}
	if int64(len(respBody)) > j.readLimitBytes {
		return fmt.Errorf("http response exceeds %d bytes", j.readLimitBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("http request failed with status %s: %s", response.Status, string(respBody))
	}
	if resp == nil || len(respBody) == 0 {
		return nil
	}

	return json.Unmarshal(respBody, resp)
}

func getCompleteURL(originURL string, params map[string]string) string {
	parsedURL, err := net_url.Parse(originURL)
	if err != nil {
		return originURL
	}
	values := parsedURL.Query()
	for k, v := range params {
		values.Set(k, v)
	}
	parsedURL.RawQuery = values.Encode()
	return parsedURL.String()
}
