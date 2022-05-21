package httpclient

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"context"

	"github.com/eapache/go-resiliency/retrier"
	"go.uber.org/zap"
)

var (
	// DefaultTimeout is the default client request timeout if not specified
	DefaultTimeout = 15 * time.Second
)

// Client is the http client handle
type Client struct {
	*http.Client
	retrier      *retrier.Retrier
	reqOpts      []RequestOption
	logger       *zap.Logger
	debugTraffic bool
}

// New creates a new http client with specified client options
func New(logger *zap.Logger, opts ...ClientOption) *Client {
	client := &Client{
		Client:       &http.Client{},
		logger:       logger,
		debugTraffic: true,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// NewJSON return a JSON client wrapper
func (client *Client) NewJSON() *JSONClient {
	return &JSONClient{client}
}

// NewXML return a XML client wrapper
func (client *Client) NewXML() *XMLClient {
	return &XMLClient{client}
}

// SetDefaultReqOpts set the default request options, applied before each request.
func (client *Client) SetDefaultReqOpts(reqOpts ...RequestOption) {
	client.reqOpts = reqOpts[:len(reqOpts):len(reqOpts)]
}

// SetRetry set the retry backoff
func (client *Client) SetRetry(backoff []time.Duration) {
	client.retrier = retrier.New(backoff, DefaultRetryClassifier)
}

// SetRetrier set the retrier
func (client *Client) SetRetrier(r *retrier.Retrier) {
	client.retrier = r
}

// Options sends the OPTIONS request
func (client *Client) Options(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "OPTIONS", url, body, reqOpts...)
}

// Head sends the HEAD request
func (client *Client) Head(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "HEAD", url, body, reqOpts...)
}

// Get sends the GET request
func (client *Client) Get(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "GET", url, body, reqOpts...)
}

// Post sends the POST request
func (client *Client) Post(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "POST", url, body, reqOpts...)
}

// Patch sends the PATCH request
func (client *Client) Patch(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "PATCH", url, body, reqOpts...)
}

// Put sends the PUT request
func (client *Client) Put(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "PUT", url, body, reqOpts...)
}

// Delete sends the DELETE request
func (client *Client) Delete(ctx context.Context, url, body string, reqOpts ...RequestOption) (result string, err error) {
	return client.Do(ctx, "DELETE", url, body, reqOpts...)
}

// Do sends a custom METHOD request
func (client *Client) Do(ctx context.Context, method, url, body string, reqOpts ...RequestOption) (result string, err error) {
	if client.retrier == nil {
		return client.do(ctx, method, url, body, reqOpts...)
	}

	err = client.retrier.Run(func() error {
		if result, err = client.do(ctx, method, url, body, reqOpts...); err != nil {
			return err
		}
		return nil
	})

	return result, err
}

// DownloadFile download file from url
func (client *Client) DownloadFile(ctx context.Context, url, outFile string, reqOpts ...RequestOption) (err error) {
	var (
		req    *http.Request
		resp   *http.Response
		method = "GET"
	)

	if req, err = http.NewRequest(method, url, nil); err != nil {
		return err
	}

	reqOpts = append(client.reqOpts, reqOpts...)

	for _, reqOpt := range reqOpts {
		if ctx, err = reqOpt(ctx, req); err != nil {
			return err
		}
	}

	if client.Timeout == 0 {
		client.Timeout = DefaultTimeout
	}

	logger := client.logger.With(
		zap.String("method", method),
		zap.String("url", req.URL.String()),
		zap.String("out_file", outFile),
	)

	begin := time.Now()
	resp, err = client.Client.Do(req)
	if err != nil {
		logger.Error("do http request", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return err
	}
	// nolint: errcheck
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = &HTTPError{resp.StatusCode, resp.Status}
		logger.Error("bad http status code", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return err
	}

	// open file
	out, err := os.Create(outFile)
	if err != nil {
		logger.Error("create download file", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		logger.Error("copy response data to download file", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return err
	}

	logger.Debug("request success", zap.Int64("file_size", written), zap.Duration("proc_time", time.Since(begin)))

	return nil

}

// do the internal request sending implementation
func (client *Client) do(ctx context.Context, method, url, body string, reqOpts ...RequestOption) (result string, err error) {
	var (
		req      *http.Request
		resp     *http.Response
		respData []byte
	)

	if req, err = http.NewRequest(method, url, strings.NewReader(body)); err != nil {
		return "", err
	}

	reqOpts = append(client.reqOpts, reqOpts...)

	for _, reqOpt := range reqOpts {
		if ctx, err = reqOpt(ctx, req); err != nil {
			return "", err
		}
	}

	if client.Timeout == 0 {
		client.Timeout = DefaultTimeout
	}

	logger := client.logger.With(
		zap.String("method", method),
		zap.String("url", req.URL.String()),
	)
	if client.debugTraffic {
		logger = logger.With(zap.String("body", body))
	}

	begin := time.Now()
	resp, err = client.Client.Do(req)
	if err != nil {
		logger.Error("do http request", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return "", err
	}
	// nolint: errcheck
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = &HTTPError{resp.StatusCode, resp.Status}
		logger.Error("bad http status code", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return "", err
	}

	var reader io.ReadCloser
	// for the case server send gzipped data even if client not sending "Accept-Encoding: gzip"
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		if reader, err = gzip.NewReader(resp.Body); err != nil {
			logger.Error("create gzip reader", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
			return "", err
		}
		defer reader.Close()
	default:
		reader = ioutil.NopCloser(resp.Body)
	}

	if respData, err = ioutil.ReadAll(reader); err != nil {
		logger.Error("read response body", zap.Error(err), zap.Duration("proc_time", time.Since(begin)))
		return "", err
	}

	result = string(respData)

	buf := &bytes.Buffer{}
	for _, cookie := range resp.Cookies() {
		buf.WriteString(fmt.Sprintf("%v=%v|", cookie.Name, cookie.Value))
	}

	if buf.Len() > 0 {
		buf.Truncate(buf.Len() - 1)
	}

	if client.debugTraffic {
		logger.Debug("request success",
			zap.String("result", result),
			zap.String("set_cookies", buf.String()),
			zap.Duration("proc_time", time.Since(begin)),
		)
	} else {
		logger.Debug("request success",
			zap.String("set_cookies", buf.String()),
			zap.Duration("proc_time", time.Since(begin)),
		)

	}

	return result, nil
}
