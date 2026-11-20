package portmap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// UPnP IGD through goupnp. The three WAN connection service variants share
// one method set, captured by igdClient so discovery can hand back any of
// them (and tests can hand in a fake device by URL).

const mappingDescription = "Link Node"

type igdClient interface {
	AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseSeconds uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	GetGenericPortMappingEntryCtx(ctx context.Context, index uint16) (remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseSeconds uint32, err error)
}

// upnpMap discovers an IGD on the LAN and maps internalPort on it.
func upnpMap(ctx context.Context, gw netip.Addr, internalPort uint16, log *slog.Logger) (*Mapping, error) {
	client, err := discoverIGD(ctx)
	if err != nil {
		return nil, err
	}
	localIP, err := localIPToward(gw)
	if err != nil {
		return nil, err
	}
	return upnpMapWith(ctx, client, localIP, internalPort, log)
}

func discoverIGD(ctx context.Context) (igdClient, error) {
	if c2, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(c2) > 0 {
		return c2[0], nil
	}
	if c1, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(c1) > 0 {
		return c1[0], nil
	}
	if cp, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx); err == nil && len(cp) > 0 {
		return cp[0], nil
	}
	return nil, errors.New("no UPnP gateway found")
}

// upnpMapWith maps internalPort through one IGD client. Split out so tests
// can inject a fake device (goupnp's ByURL constructors).
func upnpMapWith(ctx context.Context, client igdClient, localIP netip.Addr, internalPort uint16, log *slog.Logger) (*Mapping, error) {
	lifetime := RequestedLifetime
	err := upnpAdd(ctx, client, localIP, internalPort)
	if err != nil {
		// Routers with a full or confused mapping table ("table full",
		// conflict errors) often just hold stale entries. Delete only the
		// entries that are provably ours — our description AND our LAN
		// address — then retry once.
		removed := upnpCleanupStale(ctx, client, localIP)
		if removed > 0 {
			log.Info("deleted stale upnp entries of this node", "count", removed)
			err = upnpAdd(ctx, client, localIP, internalPort)
		} else if faultSaysTableFull(err) {
			// Nothing of ours to clean: the table is held by other devices.
			// That is the router owner's call, not ours to delete.
			err = fmt.Errorf("%w — the router's mapping table is full of other devices' entries; "+
				"clear the router's UPnP port mapping list or reboot the router, "+
				"then restart the node (or wait: it retries every 30 minutes)", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("upnp AddPortMapping: %w", err)
	}

	extStr, err := client.GetExternalIPAddressCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("upnp GetExternalIPAddress: %w", err)
	}
	extIP, err := netip.ParseAddr(extStr)
	if err != nil {
		return nil, fmt.Errorf("upnp returned an unusable external address %q", extStr)
	}

	m := &Mapping{
		Protocol:     "upnp",
		ExternalIP:   extIP.Unmap(),
		ExternalPort: internalPort, // UPnP maps exactly what was asked
		InternalPort: internalPort,
		Lifetime:     lifetime,
	}
	m.renew = func(ctx context.Context) (*Mapping, error) {
		return upnpMapWith(ctx, client, localIP, internalPort, log)
	}
	m.delete = func(ctx context.Context) error {
		return client.DeletePortMappingCtx(ctx, "", internalPort, "UDP")
	}
	return m, nil
}

// faultSaysTableFull recognizes a full mapping table: UPnP error 728
// (NoPortMapsAvailable), or router-specific wording on a generic code, like
// the field-observed 501 "Port mapping table id full !".
func faultSaysTableFull(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "728") || strings.Contains(s, "full")
}

// upnpAdd requests the mapping, with the permanent-lease fallback.
func upnpAdd(ctx context.Context, client igdClient, localIP netip.Addr, internalPort uint16) error {
	lease := uint32(RequestedLifetime / time.Second)
	err := client.AddPortMappingCtx(ctx, "", internalPort, "UDP", internalPort,
		localIP.String(), true, mappingDescription, lease)
	if err != nil && strings.Contains(err.Error(), "725") {
		// OnlyPermanentLeasesSupported: common router quirk. A permanent
		// mapping is still renewed (a no-op refresh) and deleted on exit.
		err = client.AddPortMappingCtx(ctx, "", internalPort, "UDP", internalPort,
			localIP.String(), true, mappingDescription, 0)
	}
	return err
}

// upnpCleanupStale walks the router's mapping table and deletes entries left
// behind by earlier runs of this node on this machine: same description AND
// same LAN address. Entries of other devices or programs are never touched.
// It returns how many entries were removed. It runs only after AddPortMapping
// failed, so an entry matching ours is stale by definition — including one
// occupying the requested port with an old internal port (the conflict case).
func upnpCleanupStale(ctx context.Context, client igdClient, localIP netip.Addr) int {
	const tableLimit = 256 // bound the walk on routers with huge tables
	type stale struct {
		remoteHost string
		extPort    uint16
		proto      string
	}
	var found []stale
	for i := 0; i < tableLimit; i++ {
		remoteHost, extPort, proto, _, intClient, _, desc, _, err :=
			client.GetGenericPortMappingEntryCtx(ctx, uint16(i))
		if err != nil {
			break // SpecifiedArrayIndexInvalid: the end of the table
		}
		if desc == mappingDescription && intClient == localIP.String() {
			found = append(found, stale{remoteHost, extPort, proto})
		}
	}
	removed := 0
	for _, s := range found {
		if client.DeletePortMappingCtx(ctx, s.remoteHost, s.extPort, s.proto) == nil {
			removed++
		}
	}
	return removed
}
