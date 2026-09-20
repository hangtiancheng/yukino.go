// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

// Single read limit: 4M
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
		opt(&j)
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
	tCtx, cancel := context.WithTimeout(ctx, j.timeoutDuration)
	defer cancel()

	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(tCtx, method, url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	for k, v := range header {
		request.Header.Add(k, v)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(response.Body, j.readLimitBytes))
	if err != nil {
		return err
	}

	return json.Unmarshal(respBody, resp)
}

func getCompleteURL(originURL string, params map[string]string) string {
	values := net_url.Values{}
	for k, v := range params {
		values.Add(k, v)
	}

	// Keep the encoded form: unescaping here would break params containing
	// reserved characters such as '&' or '='.
	queriesStr := values.Encode()
	if len(queriesStr) == 0 {
		return originURL
	}
	return fmt.Sprintf("%s?%s", originURL, queriesStr)
}
