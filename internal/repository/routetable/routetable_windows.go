//go:build windows

package routetable

import (
	"context"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Reader reads the table through IPv4; it is the shape the service layer
// depends on.
type Reader struct{}

// Read returns the Internet routing table.
func (Reader) Read(ctx context.Context) (Table, error) {
	return IPv4(ctx)
}

// IPv4 reads the Windows IPv4 forwarding table. Reading it needs no
// privilege, so the check runs before the user has approved anything. Each
// route's interface LUID is resolved to the adapter's alias and description,
// which say whether it is a TUN.
func IPv4(ctx context.Context) (Table, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read the routing table: %w", err)
	}
	var forward *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &forward); err != nil {
		return nil, fmt.Errorf("read the routing table (GetIpForwardTable2): %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(forward))

	rows := forward.Rows()
	table := make(Table, 0, len(rows))
	for _, row := range rows {
		alias, description, ifType, err := interfaceInfo(row)
		if err != nil {
			// The interface went away between the two calls; a route that
			// cannot be attributed to a live adapter cannot be the conflict.
			continue
		}
		table = append(table, Route{
			Destination: destinationText(row.DestinationPrefix),
			Interface:   alias,
			Tunnel:      TunnelAdapter(alias, description, ifType),
		})
	}
	return table, nil
}

// interfaceInfo names the adapter a route's LUID belongs to.
func interfaceInfo(row windows.MibIpForwardRow2) (string, string, uint32, error) {
	entry := windows.MibIfRow2{InterfaceLuid: row.InterfaceLuid, InterfaceIndex: row.InterfaceIndex}
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &entry); err != nil {
		return "", "", 0, fmt.Errorf("read interface %d: %w", row.InterfaceIndex, err)
	}
	return windows.UTF16ToString(entry.Alias[:]), windows.UTF16ToString(entry.Description[:]), entry.Type, nil
}

// destinationText is the destination prefix in the form ParseNetstat reads.
func destinationText(prefix windows.IpAddressPrefix) string {
	if prefix.Prefix.Family != windows.AF_INET {
		return ""
	}
	addr := (*windows.RawSockaddrInet4)(unsafe.Pointer(&prefix.Prefix)).Addr
	return netip.PrefixFrom(netip.AddrFrom4(addr), int(prefix.PrefixLength)).String()
}
