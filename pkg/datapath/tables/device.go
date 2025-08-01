// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package tables

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/cilium/statedb"
	"github.com/cilium/statedb/index"
)

var (
	DeviceIDIndex = statedb.Index[*Device, int]{
		Name: "id",
		FromObject: func(d *Device) index.KeySet {
			return index.NewKeySet(index.Int(d.Index))
		},
		FromKey:    index.Int,
		FromString: index.IntString,
		Unique:     true,
	}

	DeviceNameIndex = statedb.Index[*Device, string]{
		Name: "name",
		FromObject: func(d *Device) index.KeySet {
			return index.NewKeySet(index.String(d.Name))
		},
		FromKey:    index.String,
		FromString: index.FromString,
	}

	DeviceSelectedIndex = statedb.Index[*Device, bool]{
		Name: "selected",
		FromObject: func(d *Device) index.KeySet {
			return index.NewKeySet(index.Bool(d.Selected))
		},
		FromKey:    index.Bool,
		FromString: index.BoolString,
	}
)

func NewDeviceTable() (statedb.RWTable[*Device], error) {
	return statedb.NewTable(
		"devices",
		DeviceIDIndex,
		DeviceNameIndex,
		DeviceSelectedIndex,
	)
}

// HardwareAddr is the physical address for a network device.
// Defined here instead of using net.HardwareAddr for proper
// JSON marshalling.
type HardwareAddr []byte

func (a HardwareAddr) String() string {
	return net.HardwareAddr([]byte(a)).String()
}

func (a HardwareAddr) MarshalJSON() ([]byte, error) {
	return []byte("\"" + a.String() + "\""), nil
}

func (a *HardwareAddr) UnmarshalJSON(bs []byte) error {
	bs = bytes.Trim(bs, "\"")
	if len(bs) == 0 {
		return nil
	}
	hw, err := net.ParseMAC(string(bs))
	if err != nil {
		return err
	}
	*a = []byte(hw)
	return nil
}

// Device is a local network device along with addresses associated with it.
//
// The devices that are selected are the external facing native devices that
// Cilium will use with features such as load-balancing, host firewall and routing.
// For the selection logic applied see 'pkg/datapath/linux/devices_controller.go'.
//
// +deepequal-gen=true
type Device struct {
	Index        int             // positive integer that starts at one, zero is never used
	MTU          int             // maximum transmission unit
	Name         string          // e.g., "en0", "lo0", "eth0.100"
	HardwareAddr HardwareAddr    // IEEE MAC-48, EUI-48 and EUI-64 form
	Flags        net.Flags       // e.g. net.FlagUp, net.eFlagLoopback, net.FlagMulticast
	Addrs        []DeviceAddress // Addresses assigned to the device
	RawFlags     uint32          // Raw interface flags
	Type         string          // Device type, e.g. "veth" etc.
	MasterIndex  int             // Index of the master device (e.g. bridge or bonding device)
	OperStatus   string          // Operational status, e.g. "up", "lower-layer-down"

	Selected          bool   // True if this is an external facing device
	NotSelectedReason string // Reason why this device was not selected
}

func (d *Device) DeepCopy() *Device {
	copy := *d
	copy.Addrs = slices.Clone(d.Addrs)
	return &copy
}

func (d *Device) HasIP(ip netip.Addr) bool {
	for _, addr := range d.Addrs {
		if addr.Addr == ip {
			return true
		}
	}
	return false
}

func (*Device) TableHeader() []string {
	return []string{
		"Name",
		"Index",
		"Selected",
		"Type",
		"MTU",
		"HWAddr",
		"Flags",
		"Addresses",
		"OperStatus",
	}
}

func (d *Device) TableRow() []string {
	addrs := []string{}
	for _, addr := range d.Addrs {
		addrs = append(addrs, addr.Addr.String())
	}
	return []string{
		d.Name,
		fmt.Sprintf("%d", d.Index),
		fmt.Sprintf("%v", d.Selected),
		d.Type,
		fmt.Sprintf("%d", d.MTU),
		d.HardwareAddr.String(),
		d.Flags.String(),
		strings.Join(addrs, ", "),
		d.OperStatus,
	}
}

// NOTE: Update DeepEqual() when changing this struct.
type DeviceAddress struct {
	Addr      netip.Addr
	Secondary bool
	Scope     RouteScope // Address scope, e.g. RT_SCOPE_LINK, RT_SCOPE_HOST etc.
}

func (d *DeviceAddress) AsIP() net.IP {
	return d.Addr.AsSlice()
}

func (d *DeviceAddress) String() string {
	return fmt.Sprintf("%s (secondary=%v, scope=%d)", d.Addr, d.Secondary, d.Scope)
}

func (d *DeviceAddress) DeepEqual(other *DeviceAddress) bool {
	return d.Addr == other.Addr &&
		d.Secondary == other.Secondary &&
		d.Scope == other.Scope
}

// SelectedDevices returns the external facing network devices to use for
// load-balancing, host firewall and routing.
//
// The invalidated channel is closed when devices have changed and
// should be requeried with a new transaction.
func SelectedDevices(tbl statedb.Table[*Device], txn statedb.ReadTxn) ([]*Device, <-chan struct{}) {
	iter, invalidated := tbl.ListWatch(txn, DeviceSelectedIndex.Query(true))
	return statedb.Collect(iter), invalidated
}

// DeviceNames extracts the device names from a slice of devices.
func DeviceNames(devs []*Device) (names []string) {
	names = make([]string, len(devs))
	for i := range devs {
		names[i] = devs[i].Name
	}
	return
}

// DeviceFilter implements filtering device names either by
// concrete name ("eth0") or by iptables-like wildcard ("eth+").
type DeviceFilter []string

// NonEmpty returns true if the filter has been defined
// (i.e. user has specified --devices).
func (lst DeviceFilter) NonEmpty() bool {
	return len(lst) > 0
}

// Match checks whether the given device name matches the filter
// The first returned bool indicates there is a matched entry.
// The second returned bool indicates it's a reverse match, aka. the device should be excluded.
func (lst DeviceFilter) Match(dev string) (bool, bool) {
	for _, entry := range lst {
		reverse := false
		if strings.HasPrefix(entry, "!") {
			reverse = true
			entry = entry[1:]
		}
		if strings.HasSuffix(entry, "+") {
			prefix := strings.TrimRight(entry, "+")
			if strings.HasPrefix(dev, prefix) {
				return true, reverse
			}
		} else if dev == entry {
			return true, reverse
		}
	}
	return false, false
}
