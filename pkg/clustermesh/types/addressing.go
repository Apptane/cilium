// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package types

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"go4.org/netipx"

	"github.com/cilium/cilium/pkg/cidr"
)

//
// In this file, we define types and utilities for cluster-aware
// addressing which identifies network endpoints using IP address
// and an optional ClusterID. With this special addressing scheme,
// we can distinguish network endpoints (e.g. Pods) that have the
// same IP address, but belong to the different cluster.
//
// A "bare" IP address is still a valid identifier because there
// are cases that endpoints can be identified without ClusterID
// (e.g. network endpoint has a unique IP address). We can consider
// this as a special case that ClusterID "doesn't matter". ClusterID
// 0 is reserved for indicating that.
//

// AddrCluster is a type that holds a pair of IP and ClusterID.
// We should use this type as much as possible when we implement
// IP + Cluster addressing. We should avoid managing IP and ClusterID
// separately. Otherwise, it is very hard for code readers to see
// where we are using cluster-aware addressing.
type AddrCluster struct {
	addr      netip.Addr
	clusterID uint32
}

const AddrClusterLen = 20

var (
	errUnmarshalBadAddress   = errors.New("AddrCluster.UnmarshalJSON: bad address")
	errMarshalInvalidAddress = errors.New("AddrCluster.MarshalJSON: invalid address")

	jsonZeroAddress = []byte("\"\"")
)

// MarshalJSON marshals the address as a string in the form
// <addr>@<clusterID>, e.g. "1.2.3.4@1"
func (a *AddrCluster) MarshalJSON() ([]byte, error) {
	if !a.addr.IsValid() {
		if a.clusterID != 0 {
			return nil, errMarshalInvalidAddress
		}

		// AddrCluster{} is the zero value. Preserve this across the
		// marshalling by returning an empty string.
		return jsonZeroAddress, nil
	}

	var b bytes.Buffer
	b.WriteByte('"')
	b.WriteString(a.String())
	b.WriteByte('"')
	return b.Bytes(), nil
}

func (a *AddrCluster) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, jsonZeroAddress) {
		return nil
	}

	if len(data) <= 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errUnmarshalBadAddress
	}

	// Drop the parens
	data = data[1 : len(data)-1]

	a2, err := ParseAddrCluster(string(data))
	if err != nil {
		return err
	}
	a.addr = a2.addr
	a.clusterID = a2.clusterID

	return nil
}

// ParseAddrCluster parses s as an IP + ClusterID and returns AddrCluster.
// The string s can be a bare IP string (any IP address format allowed in
// netip.ParseAddr()) or IP string + @ + ClusterID with decimal. Bare IP
// string is considered as IP string + @ + ClusterID = 0.
func ParseAddrCluster(s string) (AddrCluster, error) {
	atIndex := strings.LastIndex(s, "@")

	var (
		addrStr      string
		clusterIDStr string
	)

	if atIndex == -1 {
		// s may be a bare IP address string, still valid
		addrStr = s
		clusterIDStr = ""
	} else {
		// s may be a IP + ClusterID string
		addrStr = s[:atIndex]
		clusterIDStr = s[atIndex+1:]
	}

	addr, err := netip.ParseAddr(addrStr)
	if err != nil {
		return AddrCluster{}, err
	}

	if clusterIDStr == "" {
		if atIndex != len(s)-1 {
			return AddrCluster{addr: addr, clusterID: 0}, nil
		} else {
			// handle the invalid case like "10.0.0.0@"
			return AddrCluster{}, fmt.Errorf("empty cluster ID")
		}
	}

	clusterID64, err := strconv.ParseUint(clusterIDStr, 10, 32)
	if err != nil {
		return AddrCluster{}, err
	}

	return AddrCluster{addr: addr, clusterID: uint32(clusterID64)}, nil
}

// MustParseAddrCluster calls ParseAddr(s) and panics on error. It is
// intended for use in tests with hard-coded strings.
func MustParseAddrCluster(s string) AddrCluster {
	addrCluster, err := ParseAddrCluster(s)
	if err != nil {
		panic(err)
	}
	return addrCluster
}

// AddrClusterFromIP parses the given net.IP using netipx.FromStdIP and returns
// AddrCluster with ClusterID = 0.
func AddrClusterFromIP(ip net.IP) (AddrCluster, bool) {
	addr, ok := netipx.FromStdIP(ip)
	if !ok {
		return AddrCluster{}, false
	}
	return AddrCluster{addr: addr, clusterID: 0}, true
}

func MustAddrClusterFromIP(ip net.IP) AddrCluster {
	addr, ok := AddrClusterFromIP(ip)
	if !ok {
		panic("cannot convert net.IP to AddrCluster")
	}
	return addr
}

// AddrClusterFrom creates AddrCluster from netip.Addr and ClusterID
func AddrClusterFrom(addr netip.Addr, clusterID uint32) AddrCluster {
	return AddrCluster{addr: addr, clusterID: clusterID}
}

// Addr returns IP address part of AddrCluster as netip.Addr. This function
// exists for keeping backward compatibility between the existing components
// which are not aware of the cluster-aware addressing. Calling this function
// against the AddrCluster which has non-zero clusterID will lose the ClusterID
// information. It should be used with an extra care.
func (ac AddrCluster) Addr() netip.Addr {
	return ac.addr
}

// ClusterID returns ClusterID part of AddrCluster as uint32. We should avoid
// using this function as much as possible and treat IP address and ClusterID
// together.
func (ac AddrCluster) ClusterID() uint32 {
	return ac.clusterID
}

// Equal returns true when given AddrCluster has a same IP address and ClusterID
func (ac0 AddrCluster) Equal(ac1 AddrCluster) bool {
	return ac0 == ac1
}

// Less compares ac0 and ac1 and returns true if ac0 is lesser than ac1
func (ac0 AddrCluster) Less(ac1 AddrCluster) bool {
	// First, compare the IP address part
	if ret := ac0.addr.Compare(ac1.addr); ret == -1 {
		return true
	} else if ret == 1 {
		return false
	} else {
		// If IP address is the same, compare ClusterID
		return ac0.clusterID < ac1.clusterID
	}
}

// This is an alias of Equal which only exists for satisfying deepequal-gen
func (ac0 *AddrCluster) DeepEqual(ac1 *AddrCluster) bool {
	return ac0.Equal(*ac1)
}

// DeepCopyInto copies in to out
func (in *AddrCluster) DeepCopyInto(out *AddrCluster) {
	if out == nil {
		return
	}
	out.addr = in.addr
	out.clusterID = in.clusterID
}

// DeepCopy returns a new copy of AddrCluster
func (in *AddrCluster) DeepCopy() *AddrCluster {
	out := new(AddrCluster)
	in.DeepCopyInto(out)
	return out
}

// String returns the string representation of the AddrCluster. If
// AddrCluster.clusterID = 0, it returns bare IP address string. Otherwise, it
// returns IP string + "@" + ClusterID (e.g. 10.0.0.1@1)
func (ac AddrCluster) String() string {
	if ac.clusterID == 0 {
		return ac.addr.String()
	}
	return ac.addr.String() + "@" + strconv.FormatUint(uint64(ac.clusterID), 10)
}

// Is4 reports whether IP address part of AddrCluster is an IPv4 address.
func (ac AddrCluster) Is4() bool {
	return ac.addr.Is4()
}

// Is6 reports whether IP address part of AddrCluster is an IPv6 address.
func (ac AddrCluster) Is6() bool {
	return ac.addr.Is6()
}

// IsUnspecified reports whether IP address part of the AddrCluster is an
// unspecified address, either the IPv4 address "0.0.0.0" or the IPv6
// address "::".
func (ac AddrCluster) IsUnspecified() bool {
	return ac.addr.IsUnspecified()
}

// As20 returns the AddrCluster in its 20-byte representation which consists
// of 16-byte IP address part from netip.Addr.As16 and 4-byte ClusterID part.
func (ac AddrCluster) As20() (ac20 [20]byte) {
	addr16 := ac.addr.As16()
	copy(ac20[:16], addr16[:])
	ac20[16] = byte(ac.clusterID >> 24)
	ac20[17] = byte(ac.clusterID >> 16)
	ac20[18] = byte(ac.clusterID >> 8)
	ac20[19] = byte(ac.clusterID)
	return ac20
}

// AsNetIP returns the IP address part of AddCluster as a net.IP type. This
// function exists for keeping backward compatibility between the existing
// components which are not aware of the cluster-aware addressing. Calling
// this function against the AddrCluster which has non-zero clusterID will
// lose the ClusterID information. It should be used with an extra care.
func (ac AddrCluster) AsNetIP() net.IP {
	return ac.addr.AsSlice()
}

func (ac AddrCluster) AsPrefixCluster() PrefixCluster {
	return PrefixClusterFrom(netip.PrefixFrom(ac.addr, ac.addr.BitLen()), WithClusterID(ac.clusterID))
}

// PrefixCluster is a type that holds a pair of prefix and ClusterID.
// We should use this type as much as possible when we implement
// prefix + Cluster addressing. We should avoid managing prefix and
// ClusterID separately. Otherwise, it is very hard for code readers
// to see where we are using cluster-aware addressing.
type PrefixCluster struct {
	prefix    netip.Prefix
	clusterID uint32
}

// NewPrefixCluster builds an instance of a PrefixCluster with the input
// prefix and clusterID.
func NewPrefixCluster(prefix netip.Prefix, clusterID uint32) PrefixCluster {
	return PrefixCluster{prefix, clusterID}
}

// NewLocalPrefixCluster builds an instance of a PrefixCluster with the input
// prefix and clusterID set to 0.
func NewLocalPrefixCluster(prefix netip.Prefix) PrefixCluster {
	return NewPrefixCluster(prefix, 0)
}

// ParsePrefixCluster parses s as an Prefix + ClusterID and returns PrefixCluster.
// The string s can be a bare IP prefix string (any prefix format allowed in
// netip.ParsePrefix()) or prefix string + @ + ClusterID with decimal. Bare prefix
// string is considered as prefix string + @ + ClusterID = 0.
func ParsePrefixCluster(s string) (PrefixCluster, error) {
	atIndex := strings.LastIndex(s, "@")

	var (
		prefixStr    string
		clusterIDStr string
	)

	if atIndex == -1 {
		// s may be a bare IP prefix string, still valid
		prefixStr = s
		clusterIDStr = ""
	} else {
		// s may be a prefix + ClusterID string
		prefixStr = s[:atIndex]
		clusterIDStr = s[atIndex+1:]
	}

	prefix, err := netip.ParsePrefix(prefixStr)
	if err != nil {
		return PrefixCluster{}, err
	}

	if clusterIDStr == "" {
		if atIndex != len(s)-1 {
			return PrefixCluster{prefix: prefix, clusterID: 0}, nil
		} else {
			// handle the invalid case like "10.0.0.0/24@"
			return PrefixCluster{}, fmt.Errorf("empty cluster ID")
		}
	}

	clusterID64, err := strconv.ParseUint(clusterIDStr, 10, 32)
	if err != nil {
		return PrefixCluster{}, err
	}

	return PrefixCluster{prefix: prefix, clusterID: uint32(clusterID64)}, nil
}

// MustParsePrefixCluster calls ParsePrefixCluster(s) and panics on error.
// It is intended for use in tests with hard-coded strings.
func MustParsePrefixCluster(s string) PrefixCluster {
	prefixCluster, err := ParsePrefixCluster(s)
	if err != nil {
		panic(err)
	}
	return prefixCluster
}

func (pc PrefixCluster) IsSingleIP() bool {
	return pc.prefix.IsSingleIP()
}

type PrefixClusterOpts func(*PrefixCluster)

func WithClusterID(id uint32) PrefixClusterOpts {
	return func(pc *PrefixCluster) { pc.clusterID = id }
}

func PrefixClusterFrom(prefix netip.Prefix, opts ...PrefixClusterOpts) PrefixCluster {
	pc := PrefixCluster{prefix: prefix}
	for _, opt := range opts {
		opt(&pc)
	}
	return pc
}

func PrefixClusterFromCIDR(c *cidr.CIDR, opts ...PrefixClusterOpts) PrefixCluster {
	if c == nil {
		return PrefixCluster{}
	}

	addr, ok := netipx.FromStdIP(c.IP)
	if !ok {
		return PrefixCluster{}
	}
	ones, _ := c.Mask.Size()

	return PrefixClusterFrom(netip.PrefixFrom(addr, ones), opts...)
}

func (pc0 PrefixCluster) Equal(pc1 PrefixCluster) bool {
	return pc0.prefix == pc1.prefix && pc0.clusterID == pc1.clusterID
}

func (pc PrefixCluster) IsValid() bool {
	return pc.prefix.IsValid()
}

func (pc PrefixCluster) AddrCluster() AddrCluster {
	return AddrClusterFrom(pc.prefix.Addr(), pc.clusterID)
}

func (pc PrefixCluster) ClusterID() uint32 {
	return pc.clusterID
}

func (pc PrefixCluster) String() string {
	if pc.clusterID == 0 {
		return pc.prefix.String()
	}
	return pc.prefix.String() + "@" + strconv.FormatUint(uint64(pc.clusterID), 10)
}

// AsPrefix returns the IP prefix part of PrefixCluster as a netip.Prefix type.
// This function exists for keeping backward compatibility between the existing
// components which are not aware of the cluster-aware addressing. Calling
// this function against the PrefixCluster which has non-zero clusterID will
// lose the ClusterID information. It should be used with an extra care.
func (pc PrefixCluster) AsPrefix() netip.Prefix {
	return netip.PrefixFrom(pc.prefix.Addr(), pc.prefix.Bits())
}

// AsIPNet returns the IP prefix part of PrefixCluster as a net.IPNet type. This
// function exists for keeping backward compatibility between the existing
// components which are not aware of the cluster-aware addressing. Calling
// this function against the PrefixCluster which has non-zero clusterID will
// lose the ClusterID information. It should be used with an extra care.
func (pc PrefixCluster) AsIPNet() net.IPNet {
	return *netipx.PrefixIPNet(pc.AsPrefix())
}

// This function is solely exists for annotating IPCache's key string with ClusterID.
// IPCache's key string is IP address or Prefix string (10.0.0.1 and 10.0.0.0/32 are
// different entry). This function assumes given string is one of those format and
// just put @<ClusterID> suffix and there's no format check for performance reason.
// User must make sure the input is a valid IP or Prefix string.
//
// We should eventually remove this function once we finish refactoring IPCache and
// stop using string as a key. At that point, we should consider using PrefixCluster
// type for IPCache's key.
func AnnotateIPCacheKeyWithClusterID(key string, clusterID uint32) string {
	return key + "@" + strconv.FormatUint(uint64(clusterID), 10)
}
