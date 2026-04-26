package httpprobe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Probe struct {
	client *http.Client
}

func NewDefault() Probe {
	return Probe{
		client: &http.Client{CheckRedirect: rejectRedirect},
	}
}

func New(client *http.Client) (Probe, error) {
	if client == nil {
		return Probe{}, errors.New("http probe client is required")
	}

	return Probe{client: client}, nil
}

func (probe Probe) Get(
	ctx context.Context,
	target outbound.DestinationURL,
	timeout time.Duration,
) result.Result[uptime.HTTPProbeResult] {
	if probe.client == nil {
		return result.Err[uptime.HTTPProbeResult](errors.New("http probe client is required"))
	}

	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, requestErr := http.NewRequestWithContext(
		requestContext,
		http.MethodGet,
		target.String(),
		nil,
	)
	if requestErr != nil {
		return result.Err[uptime.HTTPProbeResult](requestErr)
	}
	request.Header.Set("User-Agent", "error-tracker-uptime/0.1")

	startedAt := time.Now()
	response, responseErr := probe.client.Do(request)
	duration := time.Since(startedAt)
	if responseErr != nil {
		return result.Err[uptime.HTTPProbeResult](responseErr)
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))

	return uptime.NewHTTPProbeResult(response.StatusCode, duration)
}

func rejectRedirect(request *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
