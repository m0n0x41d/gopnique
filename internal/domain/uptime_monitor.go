package domain

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	monitorNameMaxBytes    = 128
	monitorMinInterval     = time.Minute
	monitorMaxInterval     = 24 * time.Hour
	monitorMinTimeout      = time.Second
	monitorMaxTimeout      = 30 * time.Second
	monitorMinGrace        = time.Minute
	monitorMaxGrace        = 24 * time.Hour
	statusPageNameMaxBytes = 128
)

type MonitorKind string

const (
	MonitorKindHTTP      MonitorKind = "http"
	MonitorKindHeartbeat MonitorKind = "heartbeat"
)

type MonitorState string

const (
	MonitorStateUnknown MonitorState = "unknown"
	MonitorStateUp      MonitorState = "up"
	MonitorStateDown    MonitorState = "down"
)

type StatusPageVisibility string

const (
	StatusPageVisibilityPrivate StatusPageVisibility = "private"
	StatusPageVisibilityPublic  StatusPageVisibility = "public"
)

type MonitorName struct {
	value string
}

type StatusPageName struct {
	value string
}

type MonitorInterval struct {
	value time.Duration
}

type MonitorTimeout struct {
	value time.Duration
}

type MonitorGrace struct {
	value time.Duration
}

type HTTPMonitorDefinition struct {
	name     MonitorName
	url      string
	interval MonitorInterval
	timeout  MonitorTimeout
}

type HeartbeatMonitorDefinition struct {
	name       MonitorName
	endpointID HeartbeatEndpointID
	interval   MonitorInterval
	grace      MonitorGrace
}

func NewHTTPMonitorDefinition(
	name string,
	targetURL string,
	interval time.Duration,
	timeout time.Duration,
) (HTTPMonitorDefinition, error) {
	monitorName, nameErr := NewMonitorName(name)
	if nameErr != nil {
		return HTTPMonitorDefinition{}, nameErr
	}

	monitorURL, urlErr := NormalizeHTTPMonitorURL(targetURL)
	if urlErr != nil {
		return HTTPMonitorDefinition{}, urlErr
	}

	monitorInterval, intervalErr := NewMonitorInterval(interval)
	if intervalErr != nil {
		return HTTPMonitorDefinition{}, intervalErr
	}

	monitorTimeout, timeoutErr := NewMonitorTimeout(timeout)
	if timeoutErr != nil {
		return HTTPMonitorDefinition{}, timeoutErr
	}

	return HTTPMonitorDefinition{
		name:     monitorName,
		url:      monitorURL,
		interval: monitorInterval,
		timeout:  monitorTimeout,
	}, nil
}

func NewHeartbeatMonitorDefinition(
	name string,
	endpointID string,
	interval time.Duration,
	grace time.Duration,
) (HeartbeatMonitorDefinition, error) {
	monitorName, nameErr := NewMonitorName(name)
	if nameErr != nil {
		return HeartbeatMonitorDefinition{}, nameErr
	}

	heartbeatEndpointID, endpointErr := NewHeartbeatEndpointID(endpointID)
	if endpointErr != nil {
		return HeartbeatMonitorDefinition{}, endpointErr
	}

	monitorInterval, intervalErr := NewMonitorInterval(interval)
	if intervalErr != nil {
		return HeartbeatMonitorDefinition{}, intervalErr
	}

	monitorGrace, graceErr := NewMonitorGrace(grace)
	if graceErr != nil {
		return HeartbeatMonitorDefinition{}, graceErr
	}

	return HeartbeatMonitorDefinition{
		name:       monitorName,
		endpointID: heartbeatEndpointID,
		interval:   monitorInterval,
		grace:      monitorGrace,
	}, nil
}

func NewMonitorName(input string) (MonitorName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return MonitorName{}, errors.New("monitor name is required")
	}

	if !utf8.ValidString(value) {
		return MonitorName{}, errors.New("monitor name must be valid utf-8")
	}

	if len(value) > monitorNameMaxBytes {
		return MonitorName{}, errors.New("monitor name is too long")
	}

	for _, char := range value {
		if unicode.IsControl(char) {
			return MonitorName{}, errors.New("monitor name must not contain control characters")
		}
	}

	return MonitorName{value: value}, nil
}

func NewStatusPageName(input string) (StatusPageName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return StatusPageName{}, errors.New("status page name is required")
	}

	if !utf8.ValidString(value) {
		return StatusPageName{}, errors.New("status page name must be valid utf-8")
	}

	if len(value) > statusPageNameMaxBytes {
		return StatusPageName{}, errors.New("status page name is too long")
	}

	for _, char := range value {
		if unicode.IsControl(char) {
			return StatusPageName{}, errors.New("status page name must not contain control characters")
		}
	}

	return StatusPageName{value: value}, nil
}

func NormalizeHTTPMonitorURL(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("monitor url is required")
	}

	parsed, parseErr := url.Parse(value)
	if parseErr != nil {
		return "", errors.New("monitor url is invalid")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("monitor url must use http or https")
	}

	if parsed.User != nil {
		return "", errors.New("monitor url userinfo is not allowed")
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("monitor url host is required")
	}

	if parsed.Fragment != "" {
		return "", errors.New("monitor url fragment is not allowed")
	}

	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)

	return parsed.String(), nil
}

func NewMonitorInterval(interval time.Duration) (MonitorInterval, error) {
	if interval < monitorMinInterval {
		return MonitorInterval{}, errors.New("monitor interval must be at least 60 seconds")
	}

	if interval > monitorMaxInterval {
		return MonitorInterval{}, errors.New("monitor interval must be at most 24 hours")
	}

	return MonitorInterval{value: interval}, nil
}

func NewMonitorTimeout(timeout time.Duration) (MonitorTimeout, error) {
	if timeout < monitorMinTimeout {
		return MonitorTimeout{}, errors.New("monitor timeout must be at least 1 second")
	}

	if timeout > monitorMaxTimeout {
		return MonitorTimeout{}, errors.New("monitor timeout must be at most 30 seconds")
	}

	return MonitorTimeout{value: timeout}, nil
}

func NewMonitorGrace(grace time.Duration) (MonitorGrace, error) {
	if grace < monitorMinGrace {
		return MonitorGrace{}, errors.New("monitor grace must be at least 60 seconds")
	}

	if grace > monitorMaxGrace {
		return MonitorGrace{}, errors.New("monitor grace must be at most 24 hours")
	}

	return MonitorGrace{value: grace}, nil
}

func ParseMonitorState(input string) (MonitorState, error) {
	state := MonitorState(strings.ToLower(strings.TrimSpace(input)))
	if state.Valid() {
		return state, nil
	}

	return "", errors.New("monitor state is invalid")
}

func ParseStatusPageVisibility(input string) (StatusPageVisibility, error) {
	visibility := StatusPageVisibility(strings.ToLower(strings.TrimSpace(input)))
	if visibility.Valid() {
		return visibility, nil
	}

	return "", errors.New("status page visibility is invalid")
}

func MonitorStateFromHTTPStatus(statusCode int) MonitorState {
	if statusCode >= 200 && statusCode < 400 {
		return MonitorStateUp
	}

	return MonitorStateDown
}

func MonitorStateChanged(previous MonitorState, next MonitorState) bool {
	return previous != next
}

func (kind MonitorKind) Valid() bool {
	return kind == MonitorKindHTTP ||
		kind == MonitorKindHeartbeat
}

func (state MonitorState) Valid() bool {
	return state == MonitorStateUnknown ||
		state == MonitorStateUp ||
		state == MonitorStateDown
}

func (visibility StatusPageVisibility) Valid() bool {
	return visibility == StatusPageVisibilityPrivate ||
		visibility == StatusPageVisibilityPublic
}

func (name MonitorName) String() string {
	return name.value
}

func (name StatusPageName) String() string {
	return name.value
}

func (interval MonitorInterval) Duration() time.Duration {
	return interval.value
}

func (timeout MonitorTimeout) Duration() time.Duration {
	return timeout.value
}

func (grace MonitorGrace) Duration() time.Duration {
	return grace.value
}

func (monitor HTTPMonitorDefinition) Name() MonitorName {
	return monitor.name
}

func (monitor HTTPMonitorDefinition) URL() string {
	return monitor.url
}

func (monitor HTTPMonitorDefinition) Interval() MonitorInterval {
	return monitor.interval
}

func (monitor HTTPMonitorDefinition) Timeout() MonitorTimeout {
	return monitor.timeout
}

func (monitor HeartbeatMonitorDefinition) Name() MonitorName {
	return monitor.name
}

func (monitor HeartbeatMonitorDefinition) EndpointID() HeartbeatEndpointID {
	return monitor.endpointID
}

func (monitor HeartbeatMonitorDefinition) Interval() MonitorInterval {
	return monitor.interval
}

func (monitor HeartbeatMonitorDefinition) Grace() MonitorGrace {
	return monitor.grace
}
