// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package config

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/vishvananda/netlink"

	"github.com/cilium/cilium/pkg/byteorder"
	"github.com/cilium/cilium/pkg/cidr"
	"github.com/cilium/cilium/pkg/datapath/link"
	dpdef "github.com/cilium/cilium/pkg/datapath/linux/config/defines"
	"github.com/cilium/cilium/pkg/datapath/linux/safenetlink"
	"github.com/cilium/cilium/pkg/datapath/linux/sysctl"
	datapathOption "github.com/cilium/cilium/pkg/datapath/option"
	"github.com/cilium/cilium/pkg/datapath/tables"
	datapath "github.com/cilium/cilium/pkg/datapath/types"
	"github.com/cilium/cilium/pkg/defaults"
	"github.com/cilium/cilium/pkg/identity"
	"github.com/cilium/cilium/pkg/kpr"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/loadbalancer"
	lbmaps "github.com/cilium/cilium/pkg/loadbalancer/maps"
	"github.com/cilium/cilium/pkg/mac"
	"github.com/cilium/cilium/pkg/maps/configmap"
	"github.com/cilium/cilium/pkg/maps/ctmap"
	ipcachemap "github.com/cilium/cilium/pkg/maps/ipcache"
	"github.com/cilium/cilium/pkg/maps/l2respondermap"
	"github.com/cilium/cilium/pkg/maps/l2v6respondermap"
	"github.com/cilium/cilium/pkg/maps/lxcmap"
	"github.com/cilium/cilium/pkg/maps/metricsmap"
	"github.com/cilium/cilium/pkg/maps/nat"
	"github.com/cilium/cilium/pkg/maps/nodemap"
	"github.com/cilium/cilium/pkg/maps/policymap"
	"github.com/cilium/cilium/pkg/maps/recorder"
	"github.com/cilium/cilium/pkg/maps/vtep"
	"github.com/cilium/cilium/pkg/netns"
	"github.com/cilium/cilium/pkg/option"
	wgtypes "github.com/cilium/cilium/pkg/wireguard/types"
)

const NodePortMaxNAT = 65535

// HeaderfileWriter is a wrapper type which implements datapath.ConfigWriter.
// It manages writing of configuration of datapath program headerfiles.
type HeaderfileWriter struct {
	log                *slog.Logger
	nodeMap            nodemap.MapV2
	nodeAddressing     datapath.NodeAddressing
	nodeExtraDefines   dpdef.Map
	nodeExtraDefineFns []dpdef.Fn
	sysctl             sysctl.Sysctl
	kprCfg             kpr.KPRConfig
}

func NewHeaderfileWriter(p WriterParams) (datapath.ConfigWriter, error) {
	merged := make(dpdef.Map)
	for _, defines := range p.NodeExtraDefines {
		if err := merged.Merge(defines); err != nil {
			return nil, err
		}
	}
	return &HeaderfileWriter{
		nodeMap:            p.NodeMap,
		nodeAddressing:     p.NodeAddressing,
		nodeExtraDefines:   merged,
		nodeExtraDefineFns: p.NodeExtraDefineFns,
		log:                p.Log,
		sysctl:             p.Sysctl,
		kprCfg:             p.KPRConfig,
	}, nil
}

func writeIncludes(w io.Writer) (int, error) {
	return fmt.Fprintf(w, "#include \"lib/utils.h\"\n\n")
}

// WriteNodeConfig writes the local node configuration to the specified writer.
//
// Deprecated: Future additions to this function will be rejected. The docs at
// https://docs.cilium.io/en/latest/contributing/development/datapath_config
// will guide you through adding new configuration.
func (h *HeaderfileWriter) WriteNodeConfig(w io.Writer, cfg *datapath.LocalNodeConfiguration) error {

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	extraMacrosMap := make(dpdef.Map)
	cDefinesMap := make(dpdef.Map)

	nativeDevices := cfg.Devices

	fw := bufio.NewWriter(w)

	writeIncludes(w)

	var ipv4NodePortAddrs, ipv6NodePortAddrs []netip.Addr
	for _, addr := range cfg.NodeAddresses {
		if !addr.NodePort {
			continue
		}
		if addr.Addr.Is4() {
			ipv4NodePortAddrs = append(ipv4NodePortAddrs, addr.Addr)
		} else {
			ipv6NodePortAddrs = append(ipv6NodePortAddrs, addr.Addr)
		}
	}

	fmt.Fprintf(fw, "/*\n")
	if option.Config.EnableIPv6 {
		fmt.Fprintf(fw, " cilium.v6.external.str %s\n", cfg.NodeIPv6.String())
		fmt.Fprintf(fw, " cilium.v6.internal.str %s\n", cfg.CiliumInternalIPv6.String())
		fmt.Fprintf(fw, " cilium.v6.nodeport.str %v\n", ipv6NodePortAddrs)
		fmt.Fprintf(fw, "\n")
	}
	fmt.Fprintf(fw, " cilium.v4.external.str %s\n", cfg.NodeIPv4.String())
	fmt.Fprintf(fw, " cilium.v4.internal.str %s\n", cfg.CiliumInternalIPv4.String())
	fmt.Fprintf(fw, " cilium.v4.nodeport.str %v\n", ipv4NodePortAddrs)
	fmt.Fprintf(fw, "\n")
	if option.Config.EnableIPv6 {
		fw.WriteString(dumpRaw(defaults.RestoreV6Addr, cfg.CiliumInternalIPv6))
	}
	fw.WriteString(dumpRaw(defaults.RestoreV4Addr, cfg.CiliumInternalIPv4))
	fmt.Fprintf(fw, " */\n\n")

	cDefinesMap["KERNEL_HZ"] = fmt.Sprintf("%d", option.Config.KernelHz)

	if option.Config.EnableIPv6 && option.Config.EnableIPv6FragmentsTracking {
		cDefinesMap["ENABLE_IPV6_FRAGMENTS"] = "1"
		cDefinesMap["CILIUM_IPV6_FRAG_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", option.Config.FragmentsMapEntries)
	}

	if option.Config.EnableIPv4 {
		ipv4GW := cfg.CiliumInternalIPv4
		ipv4Range := cfg.AllocCIDRIPv4
		cDefinesMap["IPV4_GATEWAY"] = fmt.Sprintf("%#x", byteorder.NetIPv4ToHost32(ipv4GW))
		cDefinesMap["IPV4_MASK"] = fmt.Sprintf("%#x", byteorder.NetIPv4ToHost32(net.IP(ipv4Range.Mask)))

		if option.Config.EnableIPv4FragmentsTracking {
			cDefinesMap["ENABLE_IPV4_FRAGMENTS"] = "1"
			cDefinesMap["CILIUM_IPV4_FRAG_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", option.Config.FragmentsMapEntries)
		}
	}

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	cDefinesMap["UNKNOWN_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameUnknown))
	cDefinesMap["HOST_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameHost))
	cDefinesMap["WORLD_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameWorld))
	if option.Config.IsDualStack() {
		cDefinesMap["WORLD_IPV4_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameWorldIPv4))
		cDefinesMap["WORLD_IPV6_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameWorldIPv6))
	} else {
		worldID := identity.GetReservedID(labels.IDNameWorld)
		cDefinesMap["WORLD_IPV4_ID"] = fmt.Sprintf("%d", worldID)
		cDefinesMap["WORLD_IPV6_ID"] = fmt.Sprintf("%d", worldID)
	}
	cDefinesMap["HEALTH_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameHealth))
	cDefinesMap["UNMANAGED_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameUnmanaged))
	cDefinesMap["INIT_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameInit))
	cDefinesMap["LOCAL_NODE_ID"] = fmt.Sprintf("%d", identity.ReservedIdentityRemoteNode)
	cDefinesMap["REMOTE_NODE_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameRemoteNode))
	cDefinesMap["KUBE_APISERVER_NODE_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameKubeAPIServer))
	cDefinesMap["ENCRYPTED_OVERLAY_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameEncryptedOverlay))
	cDefinesMap["CILIUM_LB_SERVICE_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBServiceMapEntries)
	cDefinesMap["CILIUM_LB_BACKENDS_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBBackendMapEntries)
	cDefinesMap["CILIUM_LB_REV_NAT_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBRevNatEntries)
	cDefinesMap["CILIUM_LB_AFFINITY_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBAffinityMapEntries)
	cDefinesMap["CILIUM_LB_SOURCE_RANGE_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBSourceRangeMapEntries)
	cDefinesMap["CILIUM_LB_MAGLEV_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", cfg.LBConfig.LBMaglevMapEntries)
	cDefinesMap["CILIUM_LB_SKIP_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", lbmaps.SkipLBMapMaxEntries)

	cDefinesMap["ENDPOINTS_MAP_SIZE"] = fmt.Sprintf("%d", lxcmap.MaxEntries)
	cDefinesMap["METRICS_MAP_SIZE"] = fmt.Sprintf("%d", metricsmap.MaxEntries)
	cDefinesMap["AUTH_MAP_SIZE"] = fmt.Sprintf("%d", option.Config.AuthMapEntries)
	cDefinesMap["CONFIG_MAP_SIZE"] = fmt.Sprintf("%d", configmap.MaxEntries)
	cDefinesMap["IPCACHE_MAP_SIZE"] = fmt.Sprintf("%d", ipcachemap.MaxEntries)
	cDefinesMap["NODE_MAP_SIZE"] = fmt.Sprintf("%d", h.nodeMap.Size())
	cDefinesMap["POLICY_PROG_MAP_SIZE"] = fmt.Sprintf("%d", policymap.PolicyCallMaxEntries)
	cDefinesMap["L2_RESPONDER_MAP4_SIZE"] = fmt.Sprintf("%d", l2respondermap.DefaultMaxEntries)
	cDefinesMap["L2_RESPONDER_MAP6_SIZE"] = fmt.Sprintf("%d", l2v6respondermap.DefaultMaxEntries)
	cDefinesMap["CT_CONNECTION_LIFETIME_TCP"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutTCP.Seconds()))
	cDefinesMap["CT_CONNECTION_LIFETIME_NONTCP"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutAny.Seconds()))
	cDefinesMap["CT_SERVICE_LIFETIME_TCP"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutSVCTCP.Seconds()))
	cDefinesMap["CT_SERVICE_LIFETIME_NONTCP"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutSVCAny.Seconds()))
	cDefinesMap["CT_SERVICE_CLOSE_REBALANCE"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutSVCTCPGrace.Seconds()))
	cDefinesMap["CT_SYN_TIMEOUT"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutSYN.Seconds()))
	cDefinesMap["CT_CLOSE_TIMEOUT"] = fmt.Sprintf("%d", int64(option.Config.CTMapEntriesTimeoutFIN.Seconds()))
	cDefinesMap["CT_REPORT_INTERVAL"] = fmt.Sprintf("%d", int64(option.Config.MonitorAggregationInterval.Seconds()))
	cDefinesMap["CT_REPORT_FLAGS"] = fmt.Sprintf("%#04x", int64(option.Config.MonitorAggregationFlags))

	if option.Config.PreAllocateMaps {
		cDefinesMap["PREALLOCATE_MAPS"] = "1"
	}
	if option.Config.BPFDistributedLRU {
		cDefinesMap["NO_COMMON_MEM_MAPS"] = "1"
	}

	cDefinesMap["EVENTS_MAP_RATE_LIMIT"] = fmt.Sprintf("%d", option.Config.BPFEventsDefaultRateLimit)
	cDefinesMap["EVENTS_MAP_BURST_LIMIT"] = fmt.Sprintf("%d", option.Config.BPFEventsDefaultBurstLimit)
	cDefinesMap["LB6_REVERSE_NAT_SK_MAP_SIZE"] = fmt.Sprintf("%d", cfg.LBConfig.LBSockRevNatEntries)
	cDefinesMap["LB4_REVERSE_NAT_SK_MAP_SIZE"] = fmt.Sprintf("%d", cfg.LBConfig.LBSockRevNatEntries)

	if h.kprCfg.EnableSessionAffinity {
		cDefinesMap["ENABLE_SESSION_AFFINITY"] = "1"
	}

	cDefinesMap["MTU"] = fmt.Sprintf("%d", cfg.DeviceMTU)

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	if option.Config.EnableIPv4 {
		cDefinesMap["ENABLE_IPV4"] = "1"
	}

	if option.Config.EnableIPv6 {
		cDefinesMap["ENABLE_IPV6"] = "1"
	}

	if option.Config.EnableSRv6 {
		cDefinesMap["ENABLE_SRV6"] = "1"
		if option.Config.SRv6EncapMode != "reduced" {
			cDefinesMap["ENABLE_SRV6_SRH_ENCAP"] = "1"
		}
	}

	if option.Config.EnableSCTP {
		cDefinesMap["ENABLE_SCTP"] = "1"
	}

	if option.Config.EnableIPSec {
		cDefinesMap["ENABLE_IPSEC"] = "1"

		if option.Config.EnableIPSecEncryptedOverlay {
			cDefinesMap["ENABLE_ENCRYPTED_OVERLAY"] = "1"
		}
	}

	if option.Config.EnableWireguard {
		cDefinesMap["ENABLE_WIREGUARD"] = "1"
		ifindex, err := link.GetIfIndex(wgtypes.IfaceName)
		if err != nil {
			return fmt.Errorf("getting %s ifindex: %w", wgtypes.IfaceName, err)
		}
		cDefinesMap["WG_IFINDEX"] = fmt.Sprintf("%d", ifindex)
		cDefinesMap["WG_PORT"] = fmt.Sprintf("%d", wgtypes.ListenPort)

		if option.Config.EncryptNode {
			cDefinesMap["ENABLE_NODE_ENCRYPTION"] = "1"
		}
	}

	if option.Config.ServiceNoBackendResponse == option.ServiceNoBackendResponseReject {
		cDefinesMap["SERVICE_NO_BACKEND_RESPONSE"] = "1"
	}

	if option.Config.EnableL2Announcements {
		cDefinesMap["ENABLE_L2_ANNOUNCEMENTS"] = "1"
		// If the agent is down for longer than the lease duration, stop responding
		cDefinesMap["L2_ANNOUNCEMENTS_MAX_LIVENESS"] = fmt.Sprintf("%dULL", option.Config.L2AnnouncerLeaseDuration.Nanoseconds())
	}

	if option.Config.EnableEncryptionStrictMode {
		cDefinesMap["ENCRYPTION_STRICT_MODE"] = "1"

		// when parsing the user input we only accept ipv4 addresses
		cDefinesMap["STRICT_IPV4_NET"] = fmt.Sprintf("%#x", byteorder.NetIPAddrToHost32(option.Config.EncryptionStrictModeCIDR.Addr()))
		cDefinesMap["STRICT_IPV4_NET_SIZE"] = fmt.Sprintf("%d", option.Config.EncryptionStrictModeCIDR.Bits())

		cDefinesMap["IPV4_ENCRYPT_IFACE"] = fmt.Sprintf("%#x", byteorder.NetIPv4ToHost32(cfg.NodeIPv4))

		ipv4Interface, ok := netip.AddrFromSlice(cfg.NodeIPv4.To4())
		if !ok {
			return fmt.Errorf("unable to parse node IPv4 address %s", cfg.NodeIPv4)
		}

		if option.Config.EncryptionStrictModeCIDR.Contains(ipv4Interface) {
			if !option.Config.EncryptionStrictModeAllowRemoteNodeIdentities {
				return fmt.Errorf(`encryption strict mode is enabled but the node's IPv4 address is within the strict CIDR range.
				This will cause the node to drop all traffic.
				Please either disable encryption or set --encryption-strict-mode-allow-dynamic-lookup=true`)
			}
			cDefinesMap["STRICT_IPV4_OVERLAPPING_CIDR"] = "1"
		}
	}

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	if option.Config.EnableBPFTProxy {
		cDefinesMap["ENABLE_TPROXY"] = "1"
	}

	if option.Config.EnableXDPPrefilter {
		cDefinesMap["ENABLE_PREFILTER"] = "1"
	}

	if option.Config.EnableEndpointRoutes {
		cDefinesMap["ENABLE_ENDPOINT_ROUTES"] = "1"
	}

	if option.Config.EnableEnvoyConfig {
		cDefinesMap["ENABLE_L7_LB"] = "1"
	}

	if h.kprCfg.EnableSocketLB {
		if option.Config.BPFSocketLBHostnsOnly {
			cDefinesMap["ENABLE_SOCKET_LB_HOST_ONLY"] = "1"
		} else {
			cDefinesMap["ENABLE_SOCKET_LB_FULL"] = "1"
		}
		if option.Config.EnableSocketLBPeer {
			cDefinesMap["ENABLE_SOCKET_LB_PEER"] = "1"
		}
		if option.Config.EnableSocketLBTracing {
			cDefinesMap["TRACE_SOCK_NOTIFY"] = "1"
		}

		if cookie, err := netns.GetNetNSCookie(); err == nil {
			// When running in nested environments (e.g. Kind), cilium-agent does
			// not run in the host netns. So, in such cases the cookie comparison
			// based on bpf_get_netns_cookie(NULL) for checking whether a socket
			// belongs to a host netns does not work.
			//
			// To fix this, we derive the cookie of the netns in which cilium-agent
			// runs via getsockopt(...SO_NETNS_COOKIE...) and then use it in the
			// check above. This is based on an assumption that cilium-agent
			// always runs with "hostNetwork: true".
			cDefinesMap["HOST_NETNS_COOKIE"] = fmt.Sprintf("%d", cookie)
		}
	}

	if option.Config.EnableLocalRedirectPolicy {
		cDefinesMap["ENABLE_LOCAL_REDIRECT_POLICY"] = "1"
	}

	cDefinesMap["NAT_46X64_PREFIX_0"] = "0"
	cDefinesMap["NAT_46X64_PREFIX_1"] = "0"
	cDefinesMap["NAT_46X64_PREFIX_2"] = "0"
	cDefinesMap["NAT_46X64_PREFIX_3"] = "0"

	if h.kprCfg.EnableNodePort {
		if option.Config.EnableHealthDatapath {
			cDefinesMap["ENABLE_HEALTH_CHECK"] = "1"
		}
		if option.Config.EnableMKE && h.kprCfg.EnableSocketLB {
			cDefinesMap["ENABLE_MKE"] = "1"
			cDefinesMap["MKE_HOST"] = fmt.Sprintf("%d", option.HostExtensionMKE)
		}
		if option.Config.EnableRecorder {
			cDefinesMap["ENABLE_CAPTURE"] = "1"
			if option.Config.EnableIPv4 {
				cDefinesMap["CAPTURE4_SIZE"] = fmt.Sprintf("%d", recorder.MapSize)
			}
			if option.Config.EnableIPv6 {
				cDefinesMap["CAPTURE6_SIZE"] = fmt.Sprintf("%d", recorder.MapSize)
			}
		}
		cDefinesMap["ENABLE_NODEPORT"] = "1"
		if option.Config.EnableIPv4 {
			cDefinesMap["NODEPORT_NEIGH4_SIZE"] = fmt.Sprintf("%d", option.Config.NeighMapEntriesGlobal)
		}
		if option.Config.EnableIPv6 {
			cDefinesMap["NODEPORT_NEIGH6_SIZE"] = fmt.Sprintf("%d", option.Config.NeighMapEntriesGlobal)
		}
		if option.Config.EnableNat46X64Gateway {
			cDefinesMap["ENABLE_NAT_46X64_GATEWAY"] = "1"
			base := option.Config.IPv6NAT46x64CIDRBase.AsSlice()
			cDefinesMap["NAT_46X64_PREFIX_0"] = fmt.Sprintf("%d", base[0])
			cDefinesMap["NAT_46X64_PREFIX_1"] = fmt.Sprintf("%d", base[1])
			cDefinesMap["NAT_46X64_PREFIX_2"] = fmt.Sprintf("%d", base[2])
			cDefinesMap["NAT_46X64_PREFIX_3"] = fmt.Sprintf("%d", base[3])
		}
		if option.Config.NodePortNat46X64 {
			cDefinesMap["ENABLE_NAT_46X64"] = "1"
		}

		// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

		const (
			dsrEncapInv = iota
			dsrEncapNone
			dsrEncapIPIP
			dsrEncapGeneve
		)
		cDefinesMap["DSR_ENCAP_IPIP"] = fmt.Sprintf("%d", dsrEncapIPIP)
		cDefinesMap["DSR_ENCAP_GENEVE"] = fmt.Sprintf("%d", dsrEncapGeneve)
		cDefinesMap["DSR_ENCAP_NONE"] = fmt.Sprintf("%d", dsrEncapNone)
		if cfg.LBConfig.LoadBalancerUsesDSR() {
			cDefinesMap["ENABLE_DSR"] = "1"
			if option.Config.EnablePMTUDiscovery {
				cDefinesMap["ENABLE_DSR_ICMP_ERRORS"] = "1"
			}
			if cfg.LBConfig.LBMode == loadbalancer.LBModeHybrid {
				cDefinesMap["ENABLE_DSR_HYBRID"] = "1"
			} else if cfg.LBConfig.LBModeAnnotation {
				cDefinesMap["ENABLE_DSR_HYBRID"] = "1"
				cDefinesMap["ENABLE_DSR_BYUSER"] = "1"
			}
			if cfg.LBConfig.DSRDispatch == loadbalancer.DSRDispatchOption {
				cDefinesMap["DSR_ENCAP_MODE"] = fmt.Sprintf("%d", dsrEncapNone)
			} else if cfg.LBConfig.DSRDispatch == loadbalancer.DSRDispatchIPIP {
				cDefinesMap["DSR_ENCAP_MODE"] = fmt.Sprintf("%d", dsrEncapIPIP)
			} else if cfg.LBConfig.DSRDispatch == loadbalancer.DSRDispatchGeneve {
				cDefinesMap["DSR_ENCAP_MODE"] = fmt.Sprintf("%d", dsrEncapGeneve)
			}
		} else {
			cDefinesMap["DSR_ENCAP_MODE"] = fmt.Sprintf("%d", dsrEncapInv)
		}
		if option.Config.EnableIPv4 {
			if option.Config.LoadBalancerRSSv4CIDR != "" {
				ipv4 := byteorder.NetIPv4ToHost32(option.Config.LoadBalancerRSSv4.IP)
				ones, _ := option.Config.LoadBalancerRSSv4.Mask.Size()
				cDefinesMap["IPV4_RSS_PREFIX"] = fmt.Sprintf("%d", ipv4)
				cDefinesMap["IPV4_RSS_PREFIX_BITS"] = fmt.Sprintf("%d", ones)
			} else {
				cDefinesMap["IPV4_RSS_PREFIX"] = "IPV4_DIRECT_ROUTING"
				cDefinesMap["IPV4_RSS_PREFIX_BITS"] = "32"
			}
		}
		if option.Config.EnableIPv6 {
			if option.Config.LoadBalancerRSSv6CIDR != "" {
				ipv6 := option.Config.LoadBalancerRSSv6.IP
				ones, _ := option.Config.LoadBalancerRSSv6.Mask.Size()
				extraMacrosMap["IPV6_RSS_PREFIX"] = ipv6.String()
				fw.WriteString(FmtDefineAddress("IPV6_RSS_PREFIX", ipv6))
				cDefinesMap["IPV6_RSS_PREFIX_BITS"] = fmt.Sprintf("%d", ones)
			} else {
				cDefinesMap["IPV6_RSS_PREFIX"] = "IPV6_DIRECT_ROUTING"
				cDefinesMap["IPV6_RSS_PREFIX_BITS"] = "128"
			}
		}

		if option.Config.NodePortAcceleration != option.NodePortAccelerationDisabled {
			cDefinesMap["ENABLE_NODEPORT_ACCELERATION"] = "1"
		}
		if !option.Config.EnableHostLegacyRouting {
			cDefinesMap["ENABLE_HOST_ROUTING"] = "1"
		}
		if h.kprCfg.EnableSVCSourceRangeCheck {
			cDefinesMap["ENABLE_SRC_RANGE_CHECK"] = "1"
			if option.Config.EnableIPv4 {
				cDefinesMap["LB4_SRC_RANGE_MAP_SIZE"] =
					fmt.Sprintf("%d", cfg.LBConfig.LBSourceRangeMapEntries)
			}
			if option.Config.EnableIPv6 {
				cDefinesMap["LB6_SRC_RANGE_MAP_SIZE"] =
					fmt.Sprintf("%d", cfg.LBConfig.LBSourceRangeMapEntries)
			}
		}

		cDefinesMap["NODEPORT_PORT_MIN"] = fmt.Sprintf("%d", cfg.LBConfig.NodePortMin)
		cDefinesMap["NODEPORT_PORT_MAX"] = fmt.Sprintf("%d", cfg.LBConfig.NodePortMax)
		cDefinesMap["NODEPORT_PORT_MIN_NAT"] = fmt.Sprintf("%d", cfg.LBConfig.NodePortMax+1)
		cDefinesMap["NODEPORT_PORT_MAX_NAT"] = strconv.Itoa(NodePortMaxNAT)
	}

	macByIfIndexMacro, isL3DevMacro, err := devMacros(nativeDevices)
	if err != nil {
		return fmt.Errorf("generating device macros: %w", err)
	}
	cDefinesMap["NATIVE_DEV_MAC_BY_IFINDEX(IFINDEX)"] = macByIfIndexMacro
	cDefinesMap["IS_L3_DEV(ifindex)"] = isL3DevMacro

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	const (
		selectionRandom = iota + 1
		selectionMaglev
	)
	cDefinesMap["LB_SELECTION_RANDOM"] = fmt.Sprintf("%d", selectionRandom)
	cDefinesMap["LB_SELECTION_MAGLEV"] = fmt.Sprintf("%d", selectionMaglev)
	if cfg.LBConfig.AlgorithmAnnotation {
		cDefinesMap["LB_SELECTION_PER_SERVICE"] = "1"
	}
	if cfg.LBConfig.LBAlgorithm == loadbalancer.LBAlgorithmRandom {
		cDefinesMap["LB_SELECTION"] = fmt.Sprintf("%d", selectionRandom)
	} else if cfg.LBConfig.LBAlgorithm == loadbalancer.LBAlgorithmMaglev {
		cDefinesMap["LB_SELECTION"] = fmt.Sprintf("%d", selectionMaglev)
	}

	// define maglev tables when loadbalancer algorith is maglev or config can
	// be set by the Service annotation
	if cfg.LBConfig.AlgorithmAnnotation ||
		cfg.LBConfig.LBAlgorithm == loadbalancer.LBAlgorithmMaglev {
		cDefinesMap["LB_MAGLEV_LUT_SIZE"] = fmt.Sprintf("%d", cfg.MaglevConfig.TableSize)
	}
	cDefinesMap["HASH_INIT4_SEED"] = fmt.Sprintf("%d", cfg.MaglevConfig.SeedJhash0)
	cDefinesMap["HASH_INIT6_SEED"] = fmt.Sprintf("%d", cfg.MaglevConfig.SeedJhash1)

	// We assume that validation for DirectRoutingDevice requirement and presence is already done
	// upstream when constructing the LocalNodeConfiguration.
	// See orchestrator/localnodeconfig.go
	drd := cfg.DirectRoutingDevice
	if drd != nil {
		if option.Config.EnableIPv4 {
			var ipv4 uint32
			for _, addr := range drd.Addrs {
				if addr.Addr.Is4() {
					ipv4 = byteorder.NetIPv4ToHost32(addr.AsIP())
					break
				}
			}
			if ipv4 == 0 {
				return fmt.Errorf("IPv4 direct routing device IP not found")
			}
			cDefinesMap["IPV4_DIRECT_ROUTING"] = fmt.Sprintf("%d", ipv4)
		}
		if option.Config.EnableIPv6 {
			ip := preferredIPv6Address(drd.Addrs)
			if ip.IsUnspecified() {
				return fmt.Errorf("IPv6 direct routing device IP not found")
			}
			extraMacrosMap["IPV6_DIRECT_ROUTING"] = ip.String()
			fw.WriteString(FmtDefineAddress("IPV6_DIRECT_ROUTING", ip.AsSlice()))
		}
	} else {
		var directRoutingIPv6 net.IP
		if option.Config.EnableIPv4 {
			cDefinesMap["IPV4_DIRECT_ROUTING"] = "0"
		}
		if option.Config.EnableIPv6 {
			extraMacrosMap["IPV6_DIRECT_ROUTING"] = directRoutingIPv6.String()
			fw.WriteString(FmtDefineAddress("IPV6_DIRECT_ROUTING", directRoutingIPv6))
		}
	}

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	if option.Config.EnableHostFirewall {
		cDefinesMap["ENABLE_HOST_FIREWALL"] = "1"
	}

	if h.kprCfg.EnableNodePort {
		if option.Config.EnableIPv4 {
			cDefinesMap["SNAT_MAPPING_IPV4_SIZE"] = fmt.Sprintf("%d", option.Config.NATMapEntriesGlobal)
		}

		if option.Config.EnableIPv6 {
			cDefinesMap["SNAT_MAPPING_IPV6_SIZE"] = fmt.Sprintf("%d", option.Config.NATMapEntriesGlobal)
		}

		cDefinesMap["SNAT_COLLISION_RETRIES"] = fmt.Sprintf("%d", nat.SnatCollisionRetries)

		if option.Config.EnableBPFMasquerade {
			if option.Config.EnableIPv4Masquerade {
				cDefinesMap["ENABLE_MASQUERADE_IPV4"] = "1"

				// ip-masq-agent depends on bpf-masq
				var excludeCIDR *cidr.CIDR
				if option.Config.EnableIPMasqAgent {
					cDefinesMap["ENABLE_IP_MASQ_AGENT_IPV4"] = "1"

					// native-routing-cidr is optional with ip-masq-agent and may be nil
					excludeCIDR = option.Config.IPv4NativeRoutingCIDR
				} else {
					excludeCIDR = cfg.NativeRoutingCIDRIPv4
				}

				if excludeCIDR != nil {
					cDefinesMap["IPV4_SNAT_EXCLUSION_DST_CIDR"] =
						fmt.Sprintf("%#x", byteorder.NetIPv4ToHost32(excludeCIDR.IP))
					ones, _ := excludeCIDR.Mask.Size()
					cDefinesMap["IPV4_SNAT_EXCLUSION_DST_CIDR_LEN"] = fmt.Sprintf("%d", ones)
				}
			}
			if option.Config.EnableIPv6Masquerade {
				cDefinesMap["ENABLE_MASQUERADE_IPV6"] = "1"

				var excludeCIDR *cidr.CIDR
				if option.Config.EnableIPMasqAgent {
					cDefinesMap["ENABLE_IP_MASQ_AGENT_IPV6"] = "1"

					excludeCIDR = option.Config.IPv6NativeRoutingCIDR
				} else {
					excludeCIDR = cfg.NativeRoutingCIDRIPv6
				}

				if excludeCIDR != nil {
					extraMacrosMap["IPV6_SNAT_EXCLUSION_DST_CIDR"] = excludeCIDR.IP.String()
					fw.WriteString(FmtDefineAddress("IPV6_SNAT_EXCLUSION_DST_CIDR", excludeCIDR.IP))
					extraMacrosMap["IPV6_SNAT_EXCLUSION_DST_CIDR_MASK"] = excludeCIDR.Mask.String()
					fw.WriteString(FmtDefineAddress("IPV6_SNAT_EXCLUSION_DST_CIDR_MASK", excludeCIDR.Mask))
				}
			}
		}
	}

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	ctmap.WriteBPFMacros(fw)

	if option.Config.AllowICMPFragNeeded {
		cDefinesMap["ALLOW_ICMP_FRAG_NEEDED"] = "1"
	}

	if option.Config.ClockSource == option.ClockSourceJiffies {
		cDefinesMap["ENABLE_JIFFIES"] = "1"
	}

	if option.Config.EnableIdentityMark {
		cDefinesMap["ENABLE_IDENTITY_MARK"] = "1"
	}

	if option.Config.EnableCustomCalls {
		cDefinesMap["ENABLE_CUSTOM_CALLS"] = "1"
	}

	if option.Config.EnableVTEP {
		cDefinesMap["ENABLE_VTEP"] = "1"
		cDefinesMap["VTEP_MAP_SIZE"] = fmt.Sprintf("%d", vtep.MaxEntries)
		cDefinesMap["VTEP_MASK"] = fmt.Sprintf("%#x", byteorder.NetIPv4ToHost32(net.IP(option.Config.VtepCidrMask)))
	}

	vlanFilter, err := vlanFilterMacros(nativeDevices)
	if err != nil {
		return fmt.Errorf("rendering vlan filter macros: %w", err)
	}
	cDefinesMap["VLAN_FILTER(ifindex, vlan_id)"] = vlanFilter

	if option.Config.DisableExternalIPMitigation {
		cDefinesMap["DISABLE_EXTERNAL_IP_MITIGATION"] = "1"
	}

	if option.Config.EnableICMPRules {
		cDefinesMap["ENABLE_ICMP_RULE"] = "1"
	}

	cDefinesMap["CIDR_IDENTITY_RANGE_START"] = fmt.Sprintf("%d", identity.MinLocalIdentity)
	cDefinesMap["CIDR_IDENTITY_RANGE_END"] = fmt.Sprintf("%d", identity.MaxLocalIdentity)

	if option.Config.TunnelingEnabled() {
		cDefinesMap["TUNNEL_MODE"] = "1"
	}

	ciliumNetLink, err := safenetlink.LinkByName(defaults.SecondHostDevice)
	if err != nil {
		return fmt.Errorf("failed to look up link '%s': %w", defaults.SecondHostDevice, err)
	}
	cDefinesMap["CILIUM_NET_MAC"] = fmt.Sprintf("{.addr=%s}", mac.CArrayString(ciliumNetLink.Attrs().HardwareAddr))
	cDefinesMap["CILIUM_NET_IFINDEX"] = fmt.Sprintf("%d", ciliumNetLink.Attrs().Index)

	ciliumHostLink, err := safenetlink.LinkByName(defaults.HostDevice)
	if err != nil {
		return fmt.Errorf("failed to look up link '%s': %w", defaults.HostDevice, err)
	}
	cDefinesMap["CILIUM_HOST_MAC"] = fmt.Sprintf("{.addr=%s}", mac.CArrayString(ciliumHostLink.Attrs().HardwareAddr))
	cDefinesMap["CILIUM_HOST_IFINDEX"] = fmt.Sprintf("%d", ciliumHostLink.Attrs().Index)

	ephemeralMin, err := getEphemeralPortRangeMin(h.sysctl)
	if err != nil {
		return fmt.Errorf("getting ephemeral port range minimun: %w", err)
	}
	cDefinesMap["EPHEMERAL_MIN"] = fmt.Sprintf("%d", ephemeralMin)

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	if err := cDefinesMap.Merge(h.nodeExtraDefines); err != nil {
		return fmt.Errorf("merging extra node defines: %w", err)
	}

	for _, fn := range h.nodeExtraDefineFns {
		defines, err := fn()
		if err != nil {
			return err
		}

		if err := cDefinesMap.Merge(defines); err != nil {
			return fmt.Errorf("merging extra node define func results: %w", err)
		}
	}

	if option.Config.EnableHealthDatapath {
		if option.Config.IPv4Enabled() {
			ipip4, err := safenetlink.LinkByName(defaults.IPIPv4Device)
			if err != nil {
				return fmt.Errorf("looking up link %s: %w", defaults.IPIPv4Device, err)
			}
			cDefinesMap["ENCAP4_IFINDEX"] = fmt.Sprintf("%d", ipip4.Attrs().Index)
		}
		if option.Config.IPv6Enabled() {
			ipip6, err := safenetlink.LinkByName(defaults.IPIPv6Device)
			if err != nil {
				return fmt.Errorf("looking up link %s: %w", defaults.IPIPv6Device, err)
			}
			cDefinesMap["ENCAP6_IFINDEX"] = fmt.Sprintf("%d", ipip6.Attrs().Index)
		}
	}

	// Write Identity and ClusterID related macros.
	cDefinesMap["CLUSTER_ID_MAX"] = fmt.Sprintf("%d", option.Config.MaxConnectedClusters)

	fmt.Fprint(fw, declareConfig("identity_length", identity.GetClusterIDShift(), "Identity length in bits"))
	fmt.Fprint(fw, assignConfig("identity_length", identity.GetClusterIDShift()))

	fmt.Fprint(fw, declareConfig("interface_ifindex", uint32(0), "ifindex of the interface the bpf program is attached to"))
	cDefinesMap["THIS_INTERFACE_IFINDEX"] = "CONFIG(interface_ifindex)"

	// --- WARNING: THIS CONFIGURATION METHOD IS DEPRECATED, SEE FUNCTION DOC ---

	// Since golang maps are unordered, we sort the keys in the map
	// to get a consistent written format to the writer. This maintains
	// the consistency when we try to calculate hash for a datapath after
	// writing the config.
	for _, key := range slices.Sorted(maps.Keys(cDefinesMap)) {
		fmt.Fprintf(fw, "#define %s %s\n", key, cDefinesMap[key])
	}

	// Populate cDefinesMap with extraMacrosMap to get all the configuration
	// in the cDefinesMap itself.
	maps.Copy(cDefinesMap, extraMacrosMap)

	// Write the JSON encoded config as base64 encoded commented string to
	// the header file.
	jsonBytes, err := json.Marshal(cDefinesMap)
	if err == nil {
		// We don't care if some error occurs while marshaling the map.
		// In such cases we skip embedding the base64 encoded JSON configuration
		// to the writer.
		encodedConfig := base64.StdEncoding.EncodeToString(jsonBytes)
		fmt.Fprintf(fw, "\n// JSON_OUTPUT: %s\n", encodedConfig)
	}

	return fw.Flush()
}

func getEphemeralPortRangeMin(sysctl sysctl.Sysctl) (int, error) {
	ephemeralPortRangeStr, err := sysctl.Read([]string{"net", "ipv4", "ip_local_port_range"})
	if err != nil {
		return 0, fmt.Errorf("unable to read net.ipv4.ip_local_port_range: %w", err)
	}
	ephemeralPortRange := strings.Split(ephemeralPortRangeStr, "\t")
	if len(ephemeralPortRange) != 2 {
		return 0, fmt.Errorf("invalid ephemeral port range: %s", ephemeralPortRangeStr)
	}
	ephemeralPortMin, err := strconv.Atoi(ephemeralPortRange[0])
	if err != nil {
		return 0, fmt.Errorf("unable to parse min port value %s for ephemeral range: %w",
			ephemeralPortRange[0], err)
	}

	return ephemeralPortMin, nil
}

// vlanFilterMacros generates VLAN_FILTER macros which
// are written to node_config.h
func vlanFilterMacros(nativeDevices []*tables.Device) (string, error) {
	devices := make(map[int]bool)
	for _, device := range nativeDevices {
		devices[device.Index] = true
	}

	allowedVlans := make(map[int]bool)
	for _, vlanId := range option.Config.VLANBPFBypass {
		allowedVlans[vlanId] = true
	}

	// allow all vlan id's
	if allowedVlans[0] {
		return "return true", nil
	}

	vlansByIfIndex := make(map[int][]int)

	links, err := safenetlink.LinkList()
	if err != nil {
		return "", fmt.Errorf("listing network interfaces: %w", err)
	}

	for _, l := range links {
		vlan, ok := l.(*netlink.Vlan)
		// if it's vlan device and we're controlling vlan main device
		// and either all vlans are allowed, or we're controlling vlan device or vlan is explicitly allowed
		if ok && devices[vlan.ParentIndex] && (devices[vlan.Index] || allowedVlans[vlan.VlanId]) {
			vlansByIfIndex[vlan.ParentIndex] = append(vlansByIfIndex[vlan.ParentIndex], vlan.VlanId)
		}
	}

	vlansCount := 0
	for _, v := range vlansByIfIndex {
		vlansCount += len(v)
		slices.Sort(v) // sort Vlanids in-place since safenetlink.LinkList() may return them in any order
	}

	if vlansCount == 0 {
		return "return false", nil
	} else if vlansCount > 5 {
		return "", fmt.Errorf("allowed VLAN list is too big - %d entries, please use '--vlan-bpf-bypass 0' in order to allow all available VLANs", vlansCount)
	} else {
		vlanFilterTmpl := template.Must(template.New("vlanFilter").Parse(
			`switch (ifindex) { \
{{range $ifindex,$vlans := . -}} case {{$ifindex}}: \
switch (vlan_id) { \
{{range $vlan := $vlans -}} case {{$vlan}}: \
{{end}}return true; \
} \
break; \
{{end}}} \
return false;`))

		var vlanFilterMacro bytes.Buffer
		if err := vlanFilterTmpl.Execute(&vlanFilterMacro, vlansByIfIndex); err != nil {
			return "", fmt.Errorf("failed to execute template: %w", err)
		}

		return vlanFilterMacro.String(), nil
	}
}

// devMacros generates NATIVE_DEV_MAC_BY_IFINDEX and IS_L3_DEV macros which
// are written to node_config.h.
func devMacros(devs []*tables.Device) (string, string, error) {
	var (
		macByIfIndexMacro, isL3DevMacroBuf bytes.Buffer
		isL3DevMacro                       string
	)
	macByIfIndex := make(map[int]string)
	l3DevIfIndices := make([]int, 0)

	for _, dev := range devs {
		if len(dev.HardwareAddr) != 6 {
			l3DevIfIndices = append(l3DevIfIndices, dev.Index)
		}
		macByIfIndex[dev.Index] = mac.CArrayString(net.HardwareAddr(dev.HardwareAddr))
	}

	macByIfindexTmpl := template.Must(template.New("macByIfIndex").Parse(
		`({ \
union macaddr __mac = {.addr = {0x0, 0x0, 0x0, 0x0, 0x0, 0x0}}; \
switch (IFINDEX) { \
{{range $idx,$mac := .}} case {{$idx}}: {union macaddr __tmp = {.addr = {{$mac}}}; __mac=__tmp;} break; \
{{end}}} \
__mac; })`))

	if err := macByIfindexTmpl.Execute(&macByIfIndexMacro, macByIfIndex); err != nil {
		return "", "", fmt.Errorf("failed to execute template: %w", err)
	}

	if len(l3DevIfIndices) == 0 {
		isL3DevMacro = "false"
	} else {
		isL3DevTmpl := template.Must(template.New("isL3Dev").Parse(
			`({ \
bool is_l3 = false; \
switch (ifindex) { \
{{range $idx := .}} case {{$idx}}: is_l3 = true; break; \
{{end}}} \
is_l3; })`))
		if err := isL3DevTmpl.Execute(&isL3DevMacroBuf, l3DevIfIndices); err != nil {
			return "", "", fmt.Errorf("failed to execute template: %w", err)
		}
		isL3DevMacro = isL3DevMacroBuf.String()
	}

	return macByIfIndexMacro.String(), isL3DevMacro, nil
}

func (h *HeaderfileWriter) writeNetdevConfig(w io.Writer, opts *option.IntOptions) {
	fmt.Fprint(w, opts.GetFmtList())

	if option.Config.EnableEndpointRoutes {
		fmt.Fprint(w, "#define USE_BPF_PROG_FOR_INGRESS_POLICY 1\n")
	}
}

// WriteNetdevConfig writes the BPF configuration for the endpoint to a writer.
func (h *HeaderfileWriter) WriteNetdevConfig(w io.Writer, opts *option.IntOptions) error {
	fw := bufio.NewWriter(w)
	h.writeNetdevConfig(fw, opts)
	return fw.Flush()
}

// WriteEndpointConfig writes the BPF configuration for the endpoint to a writer.
func (h *HeaderfileWriter) WriteEndpointConfig(w io.Writer, cfg *datapath.LocalNodeConfiguration, e datapath.EndpointConfiguration) error {
	fw := bufio.NewWriter(w)

	deviceNames := cfg.DeviceNames()

	writeIncludes(w)

	return h.writeTemplateConfig(fw, deviceNames, cfg.HostEndpointID, e, cfg.DirectRoutingDevice)
}

func (h *HeaderfileWriter) writeTemplateConfig(fw *bufio.Writer, devices []string, hostEndpointID uint64, e datapath.EndpointConfiguration, drd *tables.Device) error {
	if e.RequireEgressProg() {
		fmt.Fprintf(fw, "#define USE_BPF_PROG_FOR_INGRESS_POLICY 1\n")
	}

	if e.RequireRouting() {
		fmt.Fprintf(fw, "#define ENABLE_ROUTING 1\n")
	}

	if !option.Config.EnableHostLegacyRouting && drd != nil && len(devices) == 1 {
		if e.IsHost() || !option.Config.EnforceLXCFibLookup() {
			fmt.Fprintf(fw, "#define ENABLE_SKIP_FIB 1\n")
		}
	}

	if e.IsHost() {
		// Only used to differentiate between host endpoint template and other templates.
		fmt.Fprintf(fw, "#define HOST_ENDPOINT 1\n")
	}

	if e.IsHost() || option.Config.DatapathMode != datapathOption.DatapathModeNetkit {
		if e.RequireARPPassthrough() {
			fmt.Fprint(fw, "#define ENABLE_ARP_PASSTHROUGH 1\n")
		} else {
			fmt.Fprint(fw, "#define ENABLE_ARP_RESPONDER 1\n")
		}
	}

	// Local delivery metrics should always be set for endpoint programs.
	fmt.Fprint(fw, "#define LOCAL_DELIVERY_METRICS 1\n")

	h.writeNetdevConfig(fw, e.GetOptions())

	return fw.Flush()
}

// WriteTemplateConfig writes the BPF configuration for the template to a writer.
func (h *HeaderfileWriter) WriteTemplateConfig(w io.Writer, cfg *datapath.LocalNodeConfiguration, e datapath.EndpointConfiguration) error {
	fw := bufio.NewWriter(w)
	return h.writeTemplateConfig(fw, cfg.DeviceNames(), cfg.HostEndpointID, e, cfg.DirectRoutingDevice)
}

func preferredIPv6Address(deviceAddresses []tables.DeviceAddress) netip.Addr {
	var ip netip.Addr
	for _, addr := range deviceAddresses {
		if addr.Addr.Is6() {
			ip = addr.Addr
			if !ip.IsLinkLocalUnicast() {
				break
			}
		}
	}
	return ip
}
