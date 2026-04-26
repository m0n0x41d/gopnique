package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewHTTPMonitorDefinitionNormalizesValidMonitor(t *testing.T) {
	monitor, monitorErr := NewHTTPMonitorDefinition(
		"API health",
		"HTTPS://Status.Example.COM/health",
		time.Minute,
		5*time.Second,
	)
	if monitorErr != nil {
		t.Fatalf("monitor: %v", monitorErr)
	}

	if monitor.Name().String() != "API health" {
		t.Fatalf("unexpected name: %s", monitor.Name().String())
	}

	if monitor.URL() != "https://status.example.com/health" {
		t.Fatalf("unexpected url: %s", monitor.URL())
	}

	if monitor.Interval().Duration() != time.Minute {
		t.Fatalf("unexpected interval: %s", monitor.Interval().Duration())
	}

	if monitor.Timeout().Duration() != 5*time.Second {
		t.Fatalf("unexpected timeout: %s", monitor.Timeout().Duration())
	}
}

func TestNewHTTPMonitorDefinitionRejectsInvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		build    func() error
		contains string
	}{
		{
			name: "empty name",
			build: func() error {
				_, err := NewHTTPMonitorDefinition("", "https://status.example.com", time.Minute, time.Second)
				return err
			},
			contains: "name",
		},
		{
			name: "bad scheme",
			build: func() error {
				_, err := NewHTTPMonitorDefinition("status", "file:///tmp/status", time.Minute, time.Second)
				return err
			},
			contains: "http or https",
		},
		{
			name: "missing host",
			build: func() error {
				_, err := NewHTTPMonitorDefinition("status", "https:///health", time.Minute, time.Second)
				return err
			},
			contains: "host",
		},
		{
			name: "short interval",
			build: func() error {
				_, err := NewHTTPMonitorDefinition("status", "https://status.example.com", time.Second, time.Second)
				return err
			},
			contains: "interval",
		},
		{
			name: "long timeout",
			build: func() error {
				_, err := NewHTTPMonitorDefinition("status", "https://status.example.com", time.Minute, time.Minute)
				return err
			},
			contains: "timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %q in %q", tc.contains, err.Error())
			}
		})
	}
}

func TestNewHeartbeatMonitorDefinitionNormalizesValidMonitor(t *testing.T) {
	monitor, monitorErr := NewHeartbeatMonitorDefinition(
		"Nightly import",
		"11111111-1111-4111-a111-111111111111",
		5*time.Minute,
		2*time.Minute,
	)
	if monitorErr != nil {
		t.Fatalf("heartbeat monitor: %v", monitorErr)
	}

	if monitor.Name().String() != "Nightly import" {
		t.Fatalf("unexpected name: %s", monitor.Name().String())
	}

	if monitor.EndpointID().String() != "11111111-1111-4111-a111-111111111111" {
		t.Fatalf("unexpected endpoint id: %s", monitor.EndpointID().String())
	}

	if monitor.Interval().Duration() != 5*time.Minute {
		t.Fatalf("unexpected interval: %s", monitor.Interval().Duration())
	}

	if monitor.Grace().Duration() != 2*time.Minute {
		t.Fatalf("unexpected grace: %s", monitor.Grace().Duration())
	}
}

func TestNewHeartbeatMonitorDefinitionRejectsInvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		build    func() error
		contains string
	}{
		{
			name: "empty name",
			build: func() error {
				_, err := NewHeartbeatMonitorDefinition("", "1111111111114111a111111111111111", time.Minute, time.Minute)
				return err
			},
			contains: "name",
		},
		{
			name: "bad endpoint",
			build: func() error {
				_, err := NewHeartbeatMonitorDefinition("status", "heartbeat", time.Minute, time.Minute)
				return err
			},
			contains: "uuid",
		},
		{
			name: "short interval",
			build: func() error {
				_, err := NewHeartbeatMonitorDefinition("status", "1111111111114111a111111111111111", time.Second, time.Minute)
				return err
			},
			contains: "interval",
		},
		{
			name: "short grace",
			build: func() error {
				_, err := NewHeartbeatMonitorDefinition("status", "1111111111114111a111111111111111", time.Minute, time.Second)
				return err
			},
			contains: "grace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %q in %q", tc.contains, err.Error())
			}
		})
	}
}

func TestMonitorStateFromHTTPStatus(t *testing.T) {
	if MonitorStateFromHTTPStatus(204) != MonitorStateUp {
		t.Fatal("expected 204 to be up")
	}

	if MonitorStateFromHTTPStatus(500) != MonitorStateDown {
		t.Fatal("expected 500 to be down")
	}
}

func TestStatusPageNameAndVisibility(t *testing.T) {
	name, nameErr := NewStatusPageName("  Public status  ")
	if nameErr != nil {
		t.Fatalf("status page name: %v", nameErr)
	}

	if name.String() != "Public status" {
		t.Fatalf("unexpected status page name: %s", name.String())
	}

	visibility, visibilityErr := ParseStatusPageVisibility("PUBLIC")
	if visibilityErr != nil {
		t.Fatalf("visibility: %v", visibilityErr)
	}

	if visibility != StatusPageVisibilityPublic {
		t.Fatalf("unexpected visibility: %s", visibility)
	}
}

func TestStatusPageNameAndVisibilityRejectInvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		build    func() error
		contains string
	}{
		{
			name: "empty name",
			build: func() error {
				_, err := NewStatusPageName("")
				return err
			},
			contains: "name",
		},
		{
			name: "control character",
			build: func() error {
				_, err := NewStatusPageName("status\x01page")
				return err
			},
			contains: "control",
		},
		{
			name: "bad visibility",
			build: func() error {
				_, err := ParseStatusPageVisibility("world")
				return err
			},
			contains: "visibility",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %q in %q", tc.contains, err.Error())
			}
		})
	}
}
