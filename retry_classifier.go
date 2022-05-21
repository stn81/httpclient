package httpclient

import (
	"github.com/eapache/go-resiliency/retrier"
)

// HTTP2RetriableError defines the errors that considered retriable
var HTTP2RetriableError = []string{
	"CONNECT_ERROR",
	"PROTOCOL_ERROR",
	"STREAM_CLOSED",
}

// DefaultRetryClassifier is the default retry classifier
var DefaultRetryClassifier = &RetryClassifier{}

// RetryClassifier defines the retry error classifier
type RetryClassifier struct{}

// Classify implements the retrier.Classifier interface
func (r *RetryClassifier) Classify(err error) retrier.Action {
	if err == nil {
		return retrier.Succeed
	}

	return retrier.Retry
}
