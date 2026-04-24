package outbound

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestParseDestinationURLAcceptsHTTPAndHTTPS(t *testing.T) {
	destinationResult := ParseDestinationURL("HTTPS://Example.COM/hooks/issues")
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("parse destination: %v", destinationErr)
	}

	if destination.String() != "https://example.com/hooks/issues" {
		t.Fatalf("unexpected canonical url: %s", destination.String())
	}

	if destination.Host() != "example.com" {
		t.Fatalf("unexpected host: %s", destination.Host())
	}
}

func TestParseDestinationURLRejectsUnsafeSyntax(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		message string
	}{
		{name: "empty", input: "", message: "required"},
		{name: "unsupported scheme", input: "file:///tmp/hook", message: "http or https"},
		{name: "userinfo", input: "https://user:pass@example.com/hook", message: "userinfo"},
		{name: "missing host", input: "https:///hook", message: "host is required"},
		{name: "fragment", input: "https://example.com/hook#secret", message: "fragment"},
		{name: "localhost", input: "https://localhost/hook", message: "not allowed"},
		{name: "localhost suffix", input: "https://api.localhost/hook", message: "not allowed"},
		{name: "metadata hostname", input: "https://metadata.google.internal/hook", message: "not allowed"},
		{name: "loopback ip", input: "https://127.0.0.1/hook", message: "private address"},
		{name: "private ip", input: "https://10.0.0.1/hook", message: "private address"},
		{name: "link local ip", input: "https://169.254.169.254/latest", message: "private address"},
		{name: "unspecified ip", input: "https://0.0.0.0/hook", message: "private address"},
		{name: "private ipv6", input: "https://[fd00::1]/hook", message: "private address"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			destinationResult := ParseDestinationURL(tc.input)
			_, destinationErr := destinationResult.Value()
			if destinationErr == nil {
				t.Fatal("expected destination url to fail")
			}

			if !strings.Contains(destinationErr.Error(), tc.message) {
				t.Fatalf("expected %q, got %q", tc.message, destinationErr.Error())
			}
		})
	}
}

func TestValidateDestinationChecksResolvedAddresses(t *testing.T) {
	resolver := fakeResolver{
		"hooks.example.com": []netip.Addr{
			netip.MustParseAddr("93.184.216.34"),
		},
	}

	destinationResult := ValidateDestination(
		context.Background(),
		resolver,
		"https://hooks.example.com/error-tracker",
	)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("validate destination: %v", destinationErr)
	}

	if destination.Host() != "hooks.example.com" {
		t.Fatalf("unexpected host: %s", destination.Host())
	}
}

func TestValidateDestinationRejectsDNSRebindSensitiveAddresses(t *testing.T) {
	cases := []struct {
		name      string
		addresses []netip.Addr
	}{
		{name: "private", addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10")}},
		{name: "loopback", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "metadata", addresses: []netip.Addr{netip.MustParseAddr("169.254.169.254")}},
		{name: "mixed", addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.10")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := fakeResolver{"hooks.example.com": tc.addresses}
			destinationResult := ValidateDestination(
				context.Background(),
				resolver,
				"https://hooks.example.com/error-tracker",
			)
			_, destinationErr := destinationResult.Value()
			if destinationErr == nil {
				t.Fatal("expected destination validation to fail")
			}
		})
	}
}

func TestValidateResolvedDestinationRequiresResolverForHostnames(t *testing.T) {
	destination := mustDestination(t, "https://hooks.example.com/error-tracker")

	result := ValidateResolvedDestination(context.Background(), nil, destination)
	_, err := result.Value()
	if err == nil {
		t.Fatal("expected resolver to be required")
	}
}

type fakeResolver map[string][]netip.Addr

func (resolver fakeResolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, ok := resolver[host]
	if !ok {
		return result.Err[[]netip.Addr](errors.New("not found"))
	}

	return result.Ok(addresses)
}

func mustDestination(t *testing.T, input string) DestinationURL {
	t.Helper()

	destinationResult := ParseDestinationURL(input)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return destination
}
