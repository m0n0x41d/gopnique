package outbound

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Resolver interface {
	LookupHost(ctx context.Context, host string) result.Result[[]netip.Addr]
}

type DestinationURL struct {
	value string
	host  string
}

func ValidateDestination(
	ctx context.Context,
	resolver Resolver,
	input string,
) result.Result[DestinationURL] {
	destinationResult := ParseDestinationURL(input)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return result.Err[DestinationURL](destinationErr)
	}

	resolvedResult := ValidateResolvedDestination(ctx, resolver, destination)
	resolved, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		return result.Err[DestinationURL](resolvedErr)
	}

	return result.Ok(resolved)
}

func ParseDestinationURL(input string) result.Result[DestinationURL] {
	value := strings.TrimSpace(input)
	if value == "" {
		return result.Err[DestinationURL](errors.New("destination url is required"))
	}

	parsed, parseErr := url.Parse(value)
	if parseErr != nil {
		return result.Err[DestinationURL](errors.New("destination url is invalid"))
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return result.Err[DestinationURL](errors.New("destination url must use http or https"))
	}

	if parsed.User != nil {
		return result.Err[DestinationURL](errors.New("destination url userinfo is not allowed"))
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return result.Err[DestinationURL](errors.New("destination url host is required"))
	}

	if strings.Contains(parsed.Host, "\\") {
		return result.Err[DestinationURL](errors.New("destination url host is invalid"))
	}

	if parsed.Fragment != "" {
		return result.Err[DestinationURL](errors.New("destination url fragment is not allowed"))
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if blockedHostName(host) {
		return result.Err[DestinationURL](errors.New("destination url host is not allowed"))
	}

	if addr, ok := parseHostIP(host); ok && !allowedPublicAddr(addr) {
		return result.Err[DestinationURL](errors.New("destination url host resolves to a private address"))
	}

	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)

	return result.Ok(DestinationURL{
		value: parsed.String(),
		host:  host,
	})
}

func ValidateResolvedDestination(
	ctx context.Context,
	resolver Resolver,
	destination DestinationURL,
) result.Result[DestinationURL] {
	if destination.value == "" || destination.host == "" {
		return result.Err[DestinationURL](errors.New("destination url is required"))
	}

	if _, ok := parseHostIP(destination.host); ok {
		return result.Ok(destination)
	}

	if resolver == nil {
		return result.Err[DestinationURL](errors.New("resolver is required"))
	}

	lookupResult := resolver.LookupHost(ctx, destination.host)
	addresses, lookupErr := lookupResult.Value()
	if lookupErr != nil {
		return result.Err[DestinationURL](lookupErr)
	}

	if len(addresses) == 0 {
		return result.Err[DestinationURL](errors.New("destination url host has no addresses"))
	}

	for _, address := range addresses {
		if !allowedPublicAddr(address) {
			return result.Err[DestinationURL](errors.New("destination url host resolves to a private address"))
		}
	}

	return result.Ok(destination)
}

func parseHostIP(host string) (netip.Addr, bool) {
	addr, addrErr := netip.ParseAddr(host)
	if addrErr != nil {
		return netip.Addr{}, false
	}

	return addr, true
}

func allowedPublicAddr(addr netip.Addr) bool {
	return addr.IsValid() &&
		addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!addr.IsUnspecified()
}

func blockedHostName(host string) bool {
	return host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		host == "metadata.google.internal"
}

func (destination DestinationURL) String() string {
	return destination.value
}

func (destination DestinationURL) Host() string {
	return destination.host
}
