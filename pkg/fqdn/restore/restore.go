// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

// The restore package provides data structures important to restoring
// DNS proxy rules. This package serves as a central source for these
// structures.
// Note that these are marshaled as JSON and any changes need to be compatible
// across an upgrade!
package restore

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/cilium/cilium/pkg/u8proto"
)

// PortProtoV2 is 1 value at bit position 24.
const PortProtoV2 = 1 << 24

// ErrRemoteClusterAddr is returned when trying to parse a non local
// (i.e: not belonging to the local cluster) IP or CIDR.
var ErrRemoteClusterAddr = errors.New("IP or CIDR from remote cluster")

// PortProto is uint32 that encodes two different
// versions of port protocol keys. Version 1 is protocol
// agnostic and (naturally) encodes no values at bit
// positions 16-31. Version 2 encodes protocol at bit
// positions 16-23, and bit position 24 encodes a 1
// value to indicate that it is Version 2. Both versions
// encode the port at the
// bit positions 0-15.
//
// This works because Version 1 will naturally encode
// no values at positions 16-31 as the original Version 1
// was a uint16. Version 2 enforces a 1 value at the 24th
// bit position, so it will always be legible.
type PortProto uint32

// MakeV2PortProto returns a Version 2 port protocol.
func MakeV2PortProto(port uint16, proto u8proto.U8proto) PortProto {
	return PortProto(PortProtoV2 | (uint32(proto) << 16) | uint32(port))
}

// IsPortV2 returns true if the PortProto
// is Version 2.
func (pp PortProto) IsPortV2() bool {
	return PortProtoV2&pp == PortProtoV2
}

// Port returns the port of the PortProto
func (pp PortProto) Port() uint16 {
	return uint16(pp & 0x0000_ffff)
}

// Protocol returns the protocol of the
// PortProto. It returns "0" for Version 1.
func (pp PortProto) Protocol() uint8 {
	return uint8((pp & 0xff_0000) >> 16)
}

// ToV1 returns the Version 1 (that is, "port")
// version of the PortProto.
func (pp PortProto) ToV1() PortProto {
	return pp & 0x0000_ffff
}

// String returns the decimal representation
// of PortProtocol in string form.
func (pp PortProto) String() string {
	return fmt.Sprintf("%d", pp)
}

// DNSRules contains IP-based DNS rules for a set of port-protocols (e.g., UDP/53)
type DNSRules map[PortProto]IPRules

// IPRules is an unsorted collection of IPrules
type IPRules []IPRule

// IPRule stores the allowed destination IPs for a DNS names matching a regex
type IPRule struct {
	Re  RuleRegex
	IPs map[RuleIPOrCIDR]struct{} // IPs, nil set is wildcard and allows all IPs!
}

// RuleIPOrCIDR is one allowed destination IP or CIDR
// It marshals to/from text in a way that is compatible with net.IP and CIDRs
type RuleIPOrCIDR netip.Prefix

func ParseRuleIPOrCIDR(s string) (ip RuleIPOrCIDR, err error) {
	err = ip.UnmarshalText([]byte(s))
	return
}

func (ip RuleIPOrCIDR) ContainsAddr(addr RuleIPOrCIDR) bool {
	return addr.IsAddr() && netip.Prefix(ip).Contains(netip.Prefix(addr).Addr())
}

func (ip RuleIPOrCIDR) IsAddr() bool {
	return netip.Prefix(ip).Bits() == -1
}

func (ip RuleIPOrCIDR) Addr() netip.Addr {
	if ip.IsAddr() {
		return netip.Prefix(ip).Addr()
	} else {
		return netip.Addr{}
	}
}

func (ip RuleIPOrCIDR) String() string {
	if ip.IsAddr() {
		return netip.Prefix(ip).Addr().String()
	} else {
		return netip.Prefix(ip).String()
	}
}

func (ip RuleIPOrCIDR) ToSingleCIDR() RuleIPOrCIDR {
	addr := netip.Prefix(ip).Addr()
	return RuleIPOrCIDR(netip.PrefixFrom(addr, addr.BitLen()))
}

func (ip RuleIPOrCIDR) MarshalText() ([]byte, error) {
	if ip.IsAddr() {
		return netip.Prefix(ip).Addr().MarshalText()
	} else {
		return netip.Prefix(ip).MarshalText()
	}
}

func (ip *RuleIPOrCIDR) UnmarshalText(b []byte) (err error) {
	if b == nil {
		return errors.New("cannot unmarshal nil into RuleIPOrCIDR")
	}
	if i := bytes.IndexByte(b, byte('@')); i >= 0 {
		if i == len(b)-1 {
			return errors.New("unexpected trailing @")
		}
		clusterIDStr := string(b[i+1:])
		clusterID, err := strconv.ParseUint(clusterIDStr, 10, 32)
		if err != nil {
			return fmt.Errorf("unable to parse clusterID: %w", err)
		}
		if clusterID != 0 {
			return ErrRemoteClusterAddr
		}
		b = b[:i]
	}
	if i := bytes.IndexByte(b, byte('/')); i < 0 {
		var addr netip.Addr
		if err = addr.UnmarshalText(b); err == nil {
			*ip = RuleIPOrCIDR(netip.PrefixFrom(addr, 0xff))
		}
	} else {
		var prefix netip.Prefix
		if err = prefix.UnmarshalText(b); err == nil {
			*ip = RuleIPOrCIDR(prefix)
		}
	}
	return
}

// RuleRegex is a wrapper for a pointer to a string so that we can define marshalers for it.
type RuleRegex struct {
	Pattern *string
}

// UnmarshalText unmarshals json into a RuleRegex
// This must have a pointer receiver, otherwise the RuleRegex remains empty.
func (r *RuleRegex) UnmarshalText(b []byte) error {
	pattern := string(b)
	r.Pattern = &pattern
	return nil
}

// MarshalText marshals RuleRegex as string
func (r RuleRegex) MarshalText() ([]byte, error) {
	if r.Pattern != nil {
		return []byte(*r.Pattern), nil
	}
	return nil, nil
}
