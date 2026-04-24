package netresolver

import (
	"context"
	"net"
	"net/netip"

	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Resolver struct {
	resolver *net.Resolver
}

func New(resolver *net.Resolver) Resolver {
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	return Resolver{resolver: resolver}
}

func (resolver Resolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, lookupErr := resolver.resolver.LookupNetIP(ctx, "ip", host)
	if lookupErr != nil {
		return result.Err[[]netip.Addr](lookupErr)
	}

	return result.Ok(addresses)
}
