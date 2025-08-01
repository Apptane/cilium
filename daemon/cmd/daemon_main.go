// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/rlimit"
	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cilium/cilium/daemon/cmd/legacy"
	agentK8s "github.com/cilium/cilium/daemon/k8s"
	"github.com/cilium/cilium/pkg/aws/eni"
	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/cgroups"
	"github.com/cilium/cilium/pkg/clustermesh"
	cmtypes "github.com/cilium/cilium/pkg/clustermesh/types"
	"github.com/cilium/cilium/pkg/common"
	"github.com/cilium/cilium/pkg/controller"
	"github.com/cilium/cilium/pkg/crypto/certificatemanager"
	"github.com/cilium/cilium/pkg/datapath/linux/ipsec"
	"github.com/cilium/cilium/pkg/datapath/linux/probes"
	linuxrouting "github.com/cilium/cilium/pkg/datapath/linux/routing"
	"github.com/cilium/cilium/pkg/datapath/linux/sysctl"
	"github.com/cilium/cilium/pkg/datapath/maps"
	datapathOption "github.com/cilium/cilium/pkg/datapath/option"
	datapathTables "github.com/cilium/cilium/pkg/datapath/tables"
	"github.com/cilium/cilium/pkg/datapath/tunnel"
	datapath "github.com/cilium/cilium/pkg/datapath/types"
	"github.com/cilium/cilium/pkg/defaults"
	"github.com/cilium/cilium/pkg/endpoint"
	endpointcreator "github.com/cilium/cilium/pkg/endpoint/creator"
	endpointmetadata "github.com/cilium/cilium/pkg/endpoint/metadata"
	"github.com/cilium/cilium/pkg/endpointmanager"
	"github.com/cilium/cilium/pkg/endpointstate"
	"github.com/cilium/cilium/pkg/envoy"
	"github.com/cilium/cilium/pkg/flowdebug"
	"github.com/cilium/cilium/pkg/fqdn/bootstrap"
	"github.com/cilium/cilium/pkg/fqdn/namemanager"
	"github.com/cilium/cilium/pkg/health"
	"github.com/cilium/cilium/pkg/hive"
	"github.com/cilium/cilium/pkg/identity"
	identitycell "github.com/cilium/cilium/pkg/identity/cache/cell"
	"github.com/cilium/cilium/pkg/identity/identitymanager"
	identityrestoration "github.com/cilium/cilium/pkg/identity/restoration"
	"github.com/cilium/cilium/pkg/ipam"
	ipamOption "github.com/cilium/cilium/pkg/ipam/option"
	"github.com/cilium/cilium/pkg/ipcache"
	k8sClient "github.com/cilium/cilium/pkg/k8s/client"
	k8sSynced "github.com/cilium/cilium/pkg/k8s/synced"
	"github.com/cilium/cilium/pkg/k8s/watchers"
	"github.com/cilium/cilium/pkg/kpr"
	"github.com/cilium/cilium/pkg/kvstore"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/labelsfilter"
	"github.com/cilium/cilium/pkg/loadbalancer"
	lbmaps "github.com/cilium/cilium/pkg/loadbalancer/maps"
	"github.com/cilium/cilium/pkg/loadinfo"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
	"github.com/cilium/cilium/pkg/maglev"
	"github.com/cilium/cilium/pkg/maps/ctmap"
	"github.com/cilium/cilium/pkg/maps/nat"
	"github.com/cilium/cilium/pkg/maps/neighborsmap"
	"github.com/cilium/cilium/pkg/metrics"
	monitorAgent "github.com/cilium/cilium/pkg/monitor/agent"
	monitorAPI "github.com/cilium/cilium/pkg/monitor/api"
	"github.com/cilium/cilium/pkg/mtu"
	"github.com/cilium/cilium/pkg/node"
	nodeManager "github.com/cilium/cilium/pkg/node/manager"
	"github.com/cilium/cilium/pkg/nodediscovery"
	"github.com/cilium/cilium/pkg/option"
	"github.com/cilium/cilium/pkg/pidfile"
	"github.com/cilium/cilium/pkg/policy"
	policyDirectory "github.com/cilium/cilium/pkg/policy/directory"
	"github.com/cilium/cilium/pkg/promise"
	"github.com/cilium/cilium/pkg/time"
	"github.com/cilium/cilium/pkg/version"
	wireguard "github.com/cilium/cilium/pkg/wireguard/agent"
)

const (
	// list of supported verbose debug groups
	argDebugVerboseFlow     = "flow"
	argDebugVerboseKvstore  = "kvstore"
	argDebugVerboseEnvoy    = "envoy"
	argDebugVerboseDatapath = "datapath"
	argDebugVerbosePolicy   = "policy"

	apiTimeout   = 60 * time.Second
	daemonSubsys = "daemon"

	// fatalSleep is the duration Cilium should sleep before existing in case
	// of a log.Fatal is issued or a CLI flag is specified but does not exist.
	fatalSleep = 2 * time.Second
)

var (
	bootstrapTimestamp = time.Now()
	bootstrapStats     = bootstrapStatistics{}
)

func InitGlobalFlags(logger *slog.Logger, cmd *cobra.Command, vp *viper.Viper) {
	flags := cmd.Flags()

	// Validators
	option.Config.FixedIdentityMappingValidator = option.Validator(func(val string) error {
		vals := strings.Split(val, "=")
		if len(vals) != 2 {
			return fmt.Errorf(`invalid fixed identity: expecting "<numeric-identity>=<identity-name>" got %q`, val)
		}
		ni, err := identity.ParseNumericIdentity(vals[0])
		if err != nil {
			return fmt.Errorf(`invalid numeric identity %q: %w`, val, err)
		}
		if !identity.IsUserReservedIdentity(ni) {
			return fmt.Errorf(`invalid numeric identity %q: valid numeric identity is between %d and %d`,
				val, identity.UserReservedNumericIdentity.Uint32(), identity.MinimalNumericIdentity.Uint32())
		}
		lblStr := vals[1]
		lbl := labels.ParseLabel(lblStr)
		if lbl.IsReservedSource() {
			return fmt.Errorf(`invalid source %q for label: %s`, labels.LabelSourceReserved, lblStr)
		}
		return nil
	})

	option.Config.BPFMapEventBuffersValidator = option.Validator(func(val string) error {
		vals := strings.Split(val, "=")
		if len(vals) != 2 {
			return fmt.Errorf(`invalid bpf map event config: expecting "<map_name>=<enabled>_<max_size>_<ttl>" got %q`, val)
		}
		_, err := option.ParseEventBufferTupleString(vals[1])
		if err != nil {
			return err
		}
		return nil
	})

	option.Config.FixedZoneMappingValidator = option.Validator(func(val string) error {
		vals := strings.Split(val, "=")
		if len(vals) != 2 {
			return fmt.Errorf(`invalid fixed zone: expecting "<zone-name>=<numeric-id>" got %q`, val)
		}
		lblStr := vals[0]
		if len(lblStr) == 0 {
			return fmt.Errorf(`invalid label: %q`, lblStr)
		}
		ni, err := strconv.Atoi(vals[1])
		if err != nil {
			return fmt.Errorf(`invalid numeric ID %q: %w`, vals[1], err)
		}
		if min, max := 1, math.MaxUint8; ni < min || ni >= max {
			return fmt.Errorf(`invalid numeric ID %q: valid numeric ID is between %d and %d`, vals[1], min, max)
		}
		return nil
	})

	// Env bindings
	flags.Int(option.AgentHealthPort, defaults.AgentHealthPort, "TCP port for agent health status API")
	option.BindEnv(vp, option.AgentHealthPort)

	flags.Int(option.ClusterHealthPort, defaults.ClusterHealthPort, "TCP port for cluster-wide network connectivity health API")
	option.BindEnv(vp, option.ClusterHealthPort)

	flags.Bool(option.AllowICMPFragNeeded, defaults.AllowICMPFragNeeded, "Allow ICMP Fragmentation Needed type packets for purposes like TCP Path MTU.")
	option.BindEnv(vp, option.AllowICMPFragNeeded)

	flags.String(option.AllowLocalhost, option.AllowLocalhostAuto, "Policy when to allow local stack to reach local endpoints { auto | always | policy }")
	option.BindEnv(vp, option.AllowLocalhost)

	flags.Bool(option.AnnotateK8sNode, defaults.AnnotateK8sNode, "Annotate Kubernetes node")
	option.BindEnv(vp, option.AnnotateK8sNode)

	flags.Bool(option.AutoCreateCiliumNodeResource, defaults.AutoCreateCiliumNodeResource, "Automatically create CiliumNode resource for own node on startup")
	option.BindEnv(vp, option.AutoCreateCiliumNodeResource)

	flags.StringSlice(option.ExcludeNodeLabelPatterns, []string{}, "List of k8s node label regex patterns to be excluded from CiliumNode")
	option.BindEnv(vp, option.ExcludeNodeLabelPatterns)

	flags.String(option.BPFRoot, "", "Path to BPF filesystem")
	option.BindEnv(vp, option.BPFRoot)

	flags.Bool(option.EnableBPFClockProbe, false, "Enable BPF clock source probing for more efficient tick retrieval")
	option.BindEnv(vp, option.EnableBPFClockProbe)

	flags.String(option.CGroupRoot, "", "Path to Cgroup2 filesystem")
	option.BindEnv(vp, option.CGroupRoot)

	flags.String(option.ConfigFile, "", `Configuration file (default "$HOME/ciliumd.yaml")`)
	option.BindEnv(vp, option.ConfigFile)

	flags.String(option.ConfigDir, "", `Configuration directory that contains a file for each option`)
	option.BindEnv(vp, option.ConfigDir)

	flags.Duration(option.ConntrackGCInterval, time.Duration(0), "Overwrite the connection-tracking garbage collection interval")
	option.BindEnv(vp, option.ConntrackGCInterval)

	flags.Duration(option.ConntrackGCMaxInterval, time.Duration(0), "Set the maximum interval for the connection-tracking garbage collection")
	option.BindEnv(vp, option.ConntrackGCMaxInterval)

	flags.BoolP(option.DebugArg, "D", false, "Enable debugging mode")
	option.BindEnv(vp, option.DebugArg)

	flags.StringSlice(option.DebugVerbose, []string{}, "List of enabled verbose debug groups")
	option.BindEnv(vp, option.DebugVerbose)

	flags.String(option.DatapathMode, defaults.DatapathMode,
		fmt.Sprintf("Datapath mode name (%s, %s, %s)",
			datapathOption.DatapathModeVeth, datapathOption.DatapathModeNetkit, datapathOption.DatapathModeNetkitL2))
	option.BindEnv(vp, option.DatapathMode)

	flags.Bool(option.EnableEndpointRoutes, defaults.EnableEndpointRoutes, "Use per endpoint routes instead of routing via cilium_host")
	option.BindEnv(vp, option.EnableEndpointRoutes)

	flags.Bool(option.EnableHealthChecking, defaults.EnableHealthChecking, "Enable connectivity health checking")
	option.BindEnv(vp, option.EnableHealthChecking)

	flags.Bool(option.AgentHealthRequireK8sConnectivity, true, "Require Kubernetes connectivity in agent health endpoint")
	option.BindEnv(vp, option.AgentHealthRequireK8sConnectivity)

	flags.Bool(option.EnableHealthCheckLoadBalancerIP, defaults.EnableHealthCheckLoadBalancerIP, "Enable access of the healthcheck nodePort on the LoadBalancerIP. Needs --enable-health-check-nodeport to be enabled")
	option.BindEnv(vp, option.EnableHealthCheckLoadBalancerIP)

	flags.Bool(option.EnableEndpointHealthChecking, defaults.EnableEndpointHealthChecking, "Enable connectivity health checking between virtual endpoints")
	option.BindEnv(vp, option.EnableEndpointHealthChecking)

	flags.Int(option.HealthCheckICMPFailureThreshold, defaults.HealthCheckICMPFailureThreshold, "Number of ICMP requests sent for each run of the health checker. If at least one ICMP response is received, the node or endpoint is marked as healthy.")
	option.BindEnv(vp, option.HealthCheckICMPFailureThreshold)

	flags.Bool(option.EnableLocalNodeRoute, defaults.EnableLocalNodeRoute, "Enable installation of the route which points the allocation prefix of the local node")
	option.BindEnv(vp, option.EnableLocalNodeRoute)

	flags.Bool(option.EnableIPv4Name, defaults.EnableIPv4, "Enable IPv4 support")
	option.BindEnv(vp, option.EnableIPv4Name)

	flags.Bool(option.EnableIPv6Name, defaults.EnableIPv6, "Enable IPv6 support")
	option.BindEnv(vp, option.EnableIPv6Name)

	flags.Bool(option.EnableNat46X64Gateway, false, "Enable NAT46 and NAT64 gateway")
	option.BindEnv(vp, option.EnableNat46X64Gateway)

	flags.Bool(option.EnableIPIPTermination, false, "Enable plain IPIP/IP6IP6 termination")
	option.BindEnv(vp, option.EnableIPIPTermination)

	flags.Bool(option.EnableIPv6NDPName, defaults.EnableIPv6NDP, "Enable IPv6 NDP support")
	option.BindEnv(vp, option.EnableIPv6NDPName)

	flags.Bool(option.EnableSRv6, defaults.EnableSRv6, "Enable SRv6 support (beta)")
	flags.MarkHidden(option.EnableSRv6)
	option.BindEnv(vp, option.EnableSRv6)

	flags.String(option.SRv6EncapModeName, defaults.SRv6EncapMode, "Encapsulation mode for SRv6 (\"srh\" or \"reduced\")")
	flags.MarkHidden(option.SRv6EncapModeName)
	option.BindEnv(vp, option.SRv6EncapModeName)

	flags.Bool(option.EnableSCTPName, defaults.EnableSCTP, "Enable SCTP support (beta)")
	option.BindEnv(vp, option.EnableSCTPName)

	flags.String(option.IPv6MCastDevice, "", "Device that joins a Solicited-Node multicast group for IPv6")
	option.BindEnv(vp, option.IPv6MCastDevice)

	flags.String(option.EncryptInterface, "", "Transparent encryption interface")
	option.BindEnv(vp, option.EncryptInterface)

	flags.Bool(option.EncryptNode, defaults.EncryptNode, "Enables encrypting traffic from non-Cilium pods and host networking (only supported with WireGuard, beta)")
	option.BindEnv(vp, option.EncryptNode)

	flags.StringSlice(option.IPv4PodSubnets, []string{}, "List of IPv4 pod subnets to preconfigure for encryption")
	option.BindEnv(vp, option.IPv4PodSubnets)

	flags.StringSlice(option.IPv6PodSubnets, []string{}, "List of IPv6 pod subnets to preconfigure for encryption")
	option.BindEnv(vp, option.IPv6PodSubnets)

	flags.Var(option.NewMapOptions(&option.Config.IPAMMultiPoolPreAllocation),
		option.IPAMMultiPoolPreAllocation,
		fmt.Sprintf("Defines the minimum number of IPs a node should pre-allocate from each pool (default %s=8)", defaults.IPAMDefaultIPPool))
	vp.SetDefault(option.IPAMMultiPoolPreAllocation, "")
	option.BindEnv(vp, option.IPAMMultiPoolPreAllocation)

	flags.String(option.IPAMDefaultIPPool, defaults.IPAMDefaultIPPool, "Name of the default IP Pool when using multi-pool")
	vp.SetDefault(option.IPAMDefaultIPPool, defaults.IPAMDefaultIPPool)
	option.BindEnv(vp, option.IPAMDefaultIPPool)

	flags.StringSlice(option.ExcludeLocalAddress, []string{}, "Exclude CIDR from being recognized as local address")
	option.BindEnv(vp, option.ExcludeLocalAddress)

	flags.Bool(option.DisableCiliumEndpointCRDName, false, "Disable use of CiliumEndpoint CRD")
	option.BindEnv(vp, option.DisableCiliumEndpointCRDName)

	flags.StringSlice(option.MasqueradeInterfaces, []string{}, "Limit iptables-based egress masquerading to interfaces selector")
	option.BindEnv(vp, option.MasqueradeInterfaces)

	flags.Bool(option.BPFSocketLBHostnsOnly, false, "Skip socket LB for services when inside a pod namespace, in favor of service LB at the pod interface. Socket LB is still used when in the host namespace. Required by service mesh (e.g., Istio, Linkerd).")
	option.BindEnv(vp, option.BPFSocketLBHostnsOnly)

	flags.Bool(option.EnableSocketLBPodConnectionTermination, true, "Enable terminating connections to deleted service backends when socket-LB is enabled")
	flags.MarkHidden(option.EnableSocketLBPodConnectionTermination)
	option.BindEnv(vp, option.EnableSocketLBPodConnectionTermination)

	flags.Bool(option.EnableSocketLBTracing, true, "Enable tracing for socket-based LB")
	option.BindEnv(vp, option.EnableSocketLBTracing)

	flags.Bool(option.EnableAutoDirectRoutingName, defaults.EnableAutoDirectRouting, "Enable automatic L2 routing between nodes")
	option.BindEnv(vp, option.EnableAutoDirectRoutingName)

	flags.Bool(option.DirectRoutingSkipUnreachableName, defaults.EnableDirectRoutingSkipUnreachable, "Enable skipping L2 routes between nodes on different subnets")
	option.BindEnv(vp, option.DirectRoutingSkipUnreachableName)

	flags.Bool(option.EnableBPFTProxy, defaults.EnableBPFTProxy, "Enable BPF-based proxy redirection (beta), if support available")
	option.BindEnv(vp, option.EnableBPFTProxy)

	flags.Bool(option.EnableHostLegacyRouting, defaults.EnableHostLegacyRouting, "Enable the legacy host forwarding model which does not bypass upper stack in host namespace")
	option.BindEnv(vp, option.EnableHostLegacyRouting)

	flags.String(option.EnablePolicy, option.DefaultEnforcement, "Enable policy enforcement")
	option.BindEnv(vp, option.EnablePolicy)

	flags.Bool(option.EnableL7Proxy, defaults.EnableL7Proxy, "Enable L7 proxy for L7 policy enforcement")
	option.BindEnv(vp, option.EnableL7Proxy)

	flags.Bool(option.BPFEventsDropEnabled, defaults.BPFEventsDropEnabled, "Expose 'drop' events for Cilium monitor and/or Hubble")
	option.BindEnv(vp, option.BPFEventsDropEnabled)

	flags.Bool(option.BPFEventsPolicyVerdictEnabled, defaults.BPFEventsPolicyVerdictEnabled, "Expose 'policy verdict' events for Cilium monitor and/or Hubble")
	option.BindEnv(vp, option.BPFEventsPolicyVerdictEnabled)

	flags.Bool(option.BPFEventsTraceEnabled, defaults.BPFEventsTraceEnabled, "Expose 'trace' events for Cilium monitor and/or Hubble")
	option.BindEnv(vp, option.BPFEventsTraceEnabled)

	flags.Bool(option.EnableTracing, false, "Enable tracing while determining policy (debugging)")
	option.BindEnv(vp, option.EnableTracing)

	flags.Bool(option.BPFDistributedLRU, defaults.BPFDistributedLRU, "Enable per-CPU BPF LRU backend memory")
	option.BindEnv(vp, option.BPFDistributedLRU)

	flags.Bool(option.BPFConntrackAccounting, defaults.BPFConntrackAccounting, "Enable CT accounting for packets and bytes (default false)")
	option.BindEnv(vp, option.BPFConntrackAccounting)

	flags.Bool(option.EnableUnreachableRoutes, false, "Add unreachable routes on pod deletion")
	option.BindEnv(vp, option.EnableUnreachableRoutes)

	flags.Bool(option.EnableIPSecName, defaults.EnableIPSec, "Enable IPsec support")
	option.BindEnv(vp, option.EnableIPSecName)

	flags.String(option.IPSecKeyFileName, "", "Path to IPsec key file")
	option.BindEnv(vp, option.IPSecKeyFileName)

	flags.Duration(option.IPsecKeyRotationDuration, defaults.IPsecKeyRotationDuration, "Maximum duration of the IPsec key rotation. The previous key will be removed after that delay.")
	option.BindEnv(vp, option.IPsecKeyRotationDuration)

	flags.Bool(option.EnableIPsecKeyWatcher, defaults.EnableIPsecKeyWatcher, "Enable watcher for IPsec key. If disabled, a restart of the agent will be necessary on key rotations.")
	option.BindEnv(vp, option.EnableIPsecKeyWatcher)

	flags.Bool(option.EnableIPSecXfrmStateCaching, defaults.EnableIPSecXfrmStateCaching, "Enable XfrmState cache for IPSec. Significantly reduces CPU usage in large clusters.")
	flags.MarkHidden(option.EnableIPSecXfrmStateCaching)
	option.BindEnv(vp, option.EnableIPSecXfrmStateCaching)

	flags.Bool(option.EnableIPSecEncryptedOverlay, defaults.EnableIPSecEncryptedOverlay, "Enable IPsec encrypted overlay. If enabled tunnel traffic will be encrypted before leaving the host. Requires ipsec and tunnel mode vxlan to be enabled.")
	option.BindEnv(vp, option.EnableIPSecEncryptedOverlay)

	flags.Bool(option.EnableWireguard, false, "Enable WireGuard")
	option.BindEnv(vp, option.EnableWireguard)

	flags.Bool(option.EnableL2Announcements, false, "Enable L2 announcements")
	option.BindEnv(vp, option.EnableL2Announcements)

	flags.Duration(option.L2AnnouncerLeaseDuration, 15*time.Second, "Duration of inactivity after which a new leader is selected")
	option.BindEnv(vp, option.L2AnnouncerLeaseDuration)

	flags.Duration(option.L2AnnouncerRenewDeadline, 5*time.Second, "Interval at which the leader renews a lease")
	option.BindEnv(vp, option.L2AnnouncerRenewDeadline)

	flags.Duration(option.L2AnnouncerRetryPeriod, 2*time.Second, "Timeout after a renew failure, before the next retry")
	option.BindEnv(vp, option.L2AnnouncerRetryPeriod)

	flags.Duration(option.WireguardPersistentKeepalive, 0, "The Wireguard keepalive interval as a Go duration string")
	option.BindEnv(vp, option.WireguardPersistentKeepalive)

	flags.String(option.NodeEncryptionOptOutLabels, defaults.NodeEncryptionOptOutLabels, "Label selector for nodes which will opt-out of node-to-node encryption")
	option.BindEnv(vp, option.NodeEncryptionOptOutLabels)

	flags.Bool(option.EnableEncryptionStrictMode, false, "Enable encryption strict mode")
	option.BindEnv(vp, option.EnableEncryptionStrictMode)

	flags.String(option.EncryptionStrictModeCIDR, "", "In strict-mode encryption, all unencrypted traffic coming from this CIDR and going to this same CIDR will be dropped")
	option.BindEnv(vp, option.EncryptionStrictModeCIDR)

	flags.Bool(option.EncryptionStrictModeAllowRemoteNodeIdentities, false, "Allows unencrypted traffic from pods to remote node identities within the strict mode CIDR. This is required when tunneling is used or direct routing is used and the node CIDR and pod CIDR overlap.")
	option.BindEnv(vp, option.EncryptionStrictModeAllowRemoteNodeIdentities)

	flags.Var(option.NewMapOptions(&option.Config.FixedIdentityMapping, option.Config.FixedIdentityMappingValidator),
		option.FixedIdentityMapping, "Key-value for the fixed identity mapping which allows to use reserved label for fixed identities, e.g. 128=kv-store,129=kube-dns")
	option.BindEnv(vp, option.FixedIdentityMapping)

	flags.Duration(option.IdentityChangeGracePeriod, defaults.IdentityChangeGracePeriod, "Time to wait before using new identity on endpoint identity change")
	option.BindEnv(vp, option.IdentityChangeGracePeriod)

	flags.Duration(option.CiliumIdentityMaxJitter, defaults.CiliumIdentityMaxJitter, "Maximum jitter time to begin processing CiliumIdentity updates")
	option.BindEnv(vp, option.CiliumIdentityMaxJitter)

	flags.Duration(option.IdentityRestoreGracePeriod, defaults.IdentityRestoreGracePeriodK8s, "Time to wait before releasing unused restored CIDR identities during agent restart")
	option.BindEnv(vp, option.IdentityRestoreGracePeriod)

	flags.String(option.IdentityAllocationMode, option.IdentityAllocationModeKVstore, "Method to use for identity allocation")
	option.BindEnv(vp, option.IdentityAllocationMode)

	flags.String(option.IPAM, ipamOption.IPAMClusterPool, "Backend to use for IPAM")
	option.BindEnv(vp, option.IPAM)

	flags.String(option.IPv4Range, AutoCIDR, "Per-node IPv4 endpoint prefix, e.g. 10.16.0.0/16")
	option.BindEnv(vp, option.IPv4Range)

	flags.String(option.IPv6Range, AutoCIDR, "Per-node IPv6 endpoint prefix, e.g. fd02:1:1::/96")
	option.BindEnv(vp, option.IPv6Range)

	flags.String(option.IPv6ClusterAllocCIDRName, defaults.IPv6ClusterAllocCIDR, "IPv6 /64 CIDR used to allocate per node endpoint /96 CIDR")
	option.BindEnv(vp, option.IPv6ClusterAllocCIDRName)

	flags.String(option.IPv4ServiceRange, AutoCIDR, "Kubernetes IPv4 services CIDR if not inside cluster prefix")
	option.BindEnv(vp, option.IPv4ServiceRange)

	flags.String(option.IPv6ServiceRange, AutoCIDR, "Kubernetes IPv6 services CIDR if not inside cluster prefix")
	option.BindEnv(vp, option.IPv6ServiceRange)

	flags.String(option.K8sNamespaceName, "", "Name of the Kubernetes namespace in which Cilium is deployed in")
	option.BindEnv(vp, option.K8sNamespaceName)

	flags.String(option.AgentNotReadyNodeTaintKeyName, defaults.AgentNotReadyNodeTaint, "Key of the taint indicating that Cilium is not ready on the node")
	option.BindEnv(vp, option.AgentNotReadyNodeTaintKeyName)

	flags.Bool(option.K8sRequireIPv4PodCIDRName, false, "Require IPv4 PodCIDR to be specified in node resource")
	option.BindEnv(vp, option.K8sRequireIPv4PodCIDRName)

	flags.Bool(option.K8sRequireIPv6PodCIDRName, false, "Require IPv6 PodCIDR to be specified in node resource")
	option.BindEnv(vp, option.K8sRequireIPv6PodCIDRName)

	flags.Bool(option.KeepConfig, false, "When restoring state, keeps containers' configuration in place")
	option.BindEnv(vp, option.KeepConfig)

	flags.Duration(option.K8sSyncTimeoutName, defaults.K8sSyncTimeout, "Timeout after last K8s event for synchronizing k8s resources before exiting")
	flags.MarkHidden(option.K8sSyncTimeoutName)
	option.BindEnv(vp, option.K8sSyncTimeoutName)

	flags.Duration(option.AllocatorListTimeoutName, defaults.AllocatorListTimeout, "Timeout for listing allocator state before exiting")
	option.BindEnv(vp, option.AllocatorListTimeoutName)

	flags.String(option.LabelPrefixFile, "", "Valid label prefixes file path")
	option.BindEnv(vp, option.LabelPrefixFile)

	flags.StringSlice(option.Labels, []string{}, "List of label prefixes used to determine identity of an endpoint")
	option.BindEnv(vp, option.Labels)

	flags.String(option.AddressScopeMax, fmt.Sprintf("%d", defaults.AddressScopeMax), "Maximum local address scope for ipcache to consider host addresses")
	flags.MarkHidden(option.AddressScopeMax)
	option.BindEnv(vp, option.AddressScopeMax)

	flags.Bool(option.EnableRecorder, false, "Enable BPF datapath pcap recorder")
	flags.MarkDeprecated(option.EnableRecorder, "The feature will be removed in v1.19")
	option.BindEnv(vp, option.EnableRecorder)

	flags.Bool(option.EnableLocalRedirectPolicy, false, "Enable Local Redirect Policy")
	option.BindEnv(vp, option.EnableLocalRedirectPolicy)

	flags.Bool(option.EnableMKE, false, "Enable BPF kube-proxy replacement for MKE environments")
	flags.MarkHidden(option.EnableMKE)
	option.BindEnv(vp, option.EnableMKE)

	flags.String(option.CgroupPathMKE, "", "Cgroup v1 net_cls mount path for MKE environments")
	flags.MarkHidden(option.CgroupPathMKE)
	option.BindEnv(vp, option.CgroupPathMKE)

	flags.String(option.NodePortAcceleration, option.NodePortAccelerationDisabled, fmt.Sprintf(
		"BPF NodePort acceleration via XDP (\"%s\", \"%s\")",
		option.NodePortAccelerationNative, option.NodePortAccelerationDisabled))
	flags.MarkHidden(option.NodePortAcceleration)
	option.BindEnv(vp, option.NodePortAcceleration)

	flags.Bool(option.LoadBalancerNat46X64, false, "BPF load balancing support for NAT46 and NAT64")
	flags.MarkHidden(option.LoadBalancerNat46X64)
	option.BindEnv(vp, option.LoadBalancerNat46X64)

	flags.String(option.LoadBalancerRSSv4CIDR, "", "BPF load balancing RSS outer source IPv4 CIDR prefix for IPIP")
	option.BindEnv(vp, option.LoadBalancerRSSv4CIDR)

	flags.String(option.LoadBalancerRSSv6CIDR, "", "BPF load balancing RSS outer source IPv6 CIDR prefix for IPIP")
	option.BindEnv(vp, option.LoadBalancerRSSv6CIDR)

	flags.Bool(option.LoadBalancerIPIPSockMark, false, "BPF load balancing logic to force socket marked traffic via IPIP")
	flags.MarkHidden(option.LoadBalancerIPIPSockMark)
	option.BindEnv(vp, option.LoadBalancerIPIPSockMark)

	flags.String(option.LoadBalancerAcceleration, option.NodePortAccelerationDisabled, fmt.Sprintf(
		"BPF load balancing acceleration via XDP (\"%s\", \"%s\")",
		option.NodePortAccelerationNative, option.NodePortAccelerationDisabled))
	option.BindEnv(vp, option.LoadBalancerAcceleration)

	flags.Bool(option.EnableAutoProtectNodePortRange, true,
		"Append NodePort range to net.ipv4.ip_local_reserved_ports if it overlaps "+
			"with ephemeral port range (net.ipv4.ip_local_port_range)")
	option.BindEnv(vp, option.EnableAutoProtectNodePortRange)

	flags.Bool(option.NodePortBindProtection, true, "Reject application bind(2) requests to service ports in the NodePort range")
	option.BindEnv(vp, option.NodePortBindProtection)

	flags.Bool(option.EnableIdentityMark, true, "Enable setting identity mark for local traffic")
	option.BindEnv(vp, option.EnableIdentityMark)

	flags.Bool(option.EnableHostFirewall, false, "Enable host network policies")
	option.BindEnv(vp, option.EnableHostFirewall)

	flags.String(option.IPv4NativeRoutingCIDR, "", "Allows to explicitly specify the IPv4 CIDR for native routing. "+
		"When specified, Cilium assumes networking for this CIDR is preconfigured and hands traffic destined for that range to the Linux network stack without applying any SNAT. "+
		"Generally speaking, specifying a native routing CIDR implies that Cilium can depend on the underlying networking stack to route packets to their destination. "+
		"To offer a concrete example, if Cilium is configured to use direct routing and the Kubernetes CIDR is included in the native routing CIDR, the user must configure the routes to reach pods, either manually or by setting the auto-direct-node-routes flag.")
	option.BindEnv(vp, option.IPv4NativeRoutingCIDR)

	flags.String(option.IPv6NativeRoutingCIDR, "", "Allows to explicitly specify the IPv6 CIDR for native routing. "+
		"When specified, Cilium assumes networking for this CIDR is preconfigured and hands traffic destined for that range to the Linux network stack without applying any SNAT. "+
		"Generally speaking, specifying a native routing CIDR implies that Cilium can depend on the underlying networking stack to route packets to their destination. "+
		"To offer a concrete example, if Cilium is configured to use direct routing and the Kubernetes CIDR is included in the native routing CIDR, the user must configure the routes to reach pods, either manually or by setting the auto-direct-node-routes flag.")
	option.BindEnv(vp, option.IPv6NativeRoutingCIDR)

	flags.String(option.LibDir, defaults.LibraryPath, "Directory path to store runtime build environment")
	option.BindEnv(vp, option.LibDir)

	flags.StringSlice(option.LogDriver, []string{}, "Logging endpoints to use for example syslog")
	option.BindEnv(vp, option.LogDriver)

	flags.Var(option.NewMapOptions(&option.Config.LogOpt),
		option.LogOpt, `Log driver options for cilium-agent, `+
			`configmap example for syslog driver: {"syslog.level":"info","syslog.facility":"local5","syslog.tag":"cilium-agent"}`)
	option.BindEnv(vp, option.LogOpt)

	flags.Bool(option.LogSystemLoadConfigName, false, "Enable periodic logging of system load")
	option.BindEnv(vp, option.LogSystemLoadConfigName)

	flags.String(option.ServiceLoopbackIPv4, defaults.ServiceLoopbackIPv4, "IPv4 source address to use for SNAT "+
		"when a Pod talks to itself over a Service.")
	option.BindEnv(vp, option.ServiceLoopbackIPv4)

	flags.Bool(option.EnableIPv4Masquerade, true, "Masquerade IPv4 traffic from endpoints leaving the host")
	option.BindEnv(vp, option.EnableIPv4Masquerade)

	flags.Bool(option.EnableIPv6Masquerade, true, "Masquerade IPv6 traffic from endpoints leaving the host")
	option.BindEnv(vp, option.EnableIPv6Masquerade)

	flags.Bool(option.EnableBPFMasquerade, false, "Masquerade packets from endpoints leaving the host with BPF instead of iptables")
	option.BindEnv(vp, option.EnableBPFMasquerade)

	flags.Bool(option.EnableMasqueradeRouteSource, false, "Masquerade packets to the source IP provided from the routing layer rather than interface address")
	option.BindEnv(vp, option.EnableMasqueradeRouteSource)

	flags.Bool(option.EnableIPv4EgressGateway, false, "Enable egress gateway for IPv4")
	flags.MarkDeprecated(option.EnableIPv4EgressGateway, "Use --enable-egress-gateway instead")
	option.BindEnv(vp, option.EnableIPv4EgressGateway)

	flags.Bool(option.EnableEgressGateway, false, "Enable egress gateway")
	option.BindEnv(vp, option.EnableEgressGateway)

	flags.Bool(option.EnableEnvoyConfig, false, "Enable Envoy Config CRDs")
	option.BindEnv(vp, option.EnableEnvoyConfig)

	flags.Bool(option.InstallIptRules, true, "Install base iptables rules for cilium to mainly interact with kube-proxy (and masquerading)")
	flags.MarkHidden(option.InstallIptRules)
	option.BindEnv(vp, option.InstallIptRules)

	flags.Uint(option.MaxCtrlIntervalName, 0, "Maximum interval (in seconds) between controller runs. Zero is no limit.")
	flags.MarkHidden(option.MaxCtrlIntervalName)
	option.BindEnv(vp, option.MaxCtrlIntervalName)

	flags.String(option.MonitorAggregationName, "None",
		"Level of monitor aggregation for traces from the datapath")
	option.BindEnvWithLegacyEnvFallback(vp, option.MonitorAggregationName, "CILIUM_MONITOR_AGGREGATION_LEVEL")

	flags.Int(option.MTUName, 0, "Overwrite auto-detected MTU of underlying network")
	option.BindEnv(vp, option.MTUName)

	flags.Int(option.RouteMetric, 0, "Overwrite the metric used by cilium when adding routes to its 'cilium_host' device")
	option.BindEnv(vp, option.RouteMetric)

	flags.String(option.IPv6NodeAddr, "auto", "IPv6 address of node")
	option.BindEnv(vp, option.IPv6NodeAddr)

	flags.String(option.IPv4NodeAddr, "auto", "IPv4 address of node")
	option.BindEnv(vp, option.IPv4NodeAddr)

	flags.Bool(option.Restore, true, "Restores state, if possible, from previous daemon")
	flags.MarkHidden(option.Restore)
	option.BindEnv(vp, option.Restore)

	flags.String(option.SocketPath, defaults.SockPath, "Sets daemon's socket path to listen for connections")
	option.BindEnv(vp, option.SocketPath)

	flags.String(option.StateDir, defaults.RuntimePath, "Directory path to store runtime state")
	option.BindEnv(vp, option.StateDir)

	flags.Bool(option.ExternalEnvoyProxy, false, "whether the Envoy is deployed externally in form of a DaemonSet or not")
	option.BindEnv(vp, option.ExternalEnvoyProxy)

	flags.String(option.RoutingMode, defaults.RoutingMode, fmt.Sprintf("Routing mode (%q or %q)", option.RoutingModeNative, option.RoutingModeTunnel))
	option.BindEnv(vp, option.RoutingMode)

	flags.String(option.ServiceNoBackendResponse, defaults.ServiceNoBackendResponse, "Response to traffic for a service without backends")
	option.BindEnv(vp, option.ServiceNoBackendResponse)

	flags.Int(option.TracePayloadlen, defaults.TracePayloadLen, "Length of payload to capture when tracing native packets.")
	option.BindEnv(vp, option.TracePayloadlen)

	flags.Int(option.TracePayloadlenOverlay, defaults.TracePayloadLenOverlay, "Length of payload to capture when tracing overlay packets.")
	option.BindEnv(vp, option.TracePayloadlenOverlay)

	flags.Bool(option.Version, false, "Print version information")
	option.BindEnv(vp, option.Version)

	flags.Bool(option.EnableXDPPrefilter, false, "Enable XDP prefiltering")
	option.BindEnv(vp, option.EnableXDPPrefilter)

	flags.Bool(option.EnableTCX, true, "Attach endpoint programs using tcx if supported by the kernel")
	option.BindEnv(vp, option.EnableTCX)

	flags.Bool(option.PreAllocateMapsName, defaults.PreAllocateMaps, "Enable BPF map pre-allocation")
	option.BindEnv(vp, option.PreAllocateMapsName)

	flags.Int(option.AuthMapEntriesName, option.AuthMapEntriesDefault, "Maximum number of entries in auth map")
	option.BindEnv(vp, option.AuthMapEntriesName)

	flags.Int(option.CTMapEntriesGlobalTCPName, option.CTMapEntriesGlobalTCPDefault, "Maximum number of entries in TCP CT table")
	option.BindEnvWithLegacyEnvFallback(vp, option.CTMapEntriesGlobalTCPName, "CILIUM_GLOBAL_CT_MAX_TCP")

	flags.Int(option.CTMapEntriesGlobalAnyName, option.CTMapEntriesGlobalAnyDefault, "Maximum number of entries in non-TCP CT table")
	option.BindEnvWithLegacyEnvFallback(vp, option.CTMapEntriesGlobalAnyName, "CILIUM_GLOBAL_CT_MAX_ANY")

	flags.Duration(option.CTMapEntriesTimeoutTCPName, 8000*time.Second, "Timeout for established entries in TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutTCPName)

	flags.Duration(option.CTMapEntriesTimeoutAnyName, 60*time.Second, "Timeout for entries in non-TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutAnyName)

	flags.Duration(option.CTMapEntriesTimeoutSVCTCPName, 8000*time.Second, "Timeout for established service entries in TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutSVCTCPName)

	flags.Duration(option.CTMapEntriesTimeoutSVCTCPGraceName, 60*time.Second, "Timeout for graceful shutdown of service entries in TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutSVCTCPGraceName)

	flags.Duration(option.CTMapEntriesTimeoutSVCAnyName, 60*time.Second, "Timeout for service entries in non-TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutSVCAnyName)

	flags.Duration(option.CTMapEntriesTimeoutSYNName, 60*time.Second, "Establishment timeout for entries in TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutSYNName)

	flags.Duration(option.CTMapEntriesTimeoutFINName, 10*time.Second, "Teardown timeout for entries in TCP CT table")
	option.BindEnv(vp, option.CTMapEntriesTimeoutFINName)

	flags.Duration(option.MonitorAggregationInterval, 5*time.Second, "Monitor report interval when monitor aggregation is enabled")
	option.BindEnv(vp, option.MonitorAggregationInterval)

	flags.StringSlice(option.MonitorAggregationFlags, option.MonitorAggregationFlagsDefault, "TCP flags that trigger monitor reports when monitor aggregation is enabled")
	option.BindEnv(vp, option.MonitorAggregationFlags)

	flags.Int(option.NATMapEntriesGlobalName, option.NATMapEntriesGlobalDefault, "Maximum number of entries for the global BPF NAT table")
	option.BindEnv(vp, option.NATMapEntriesGlobalName)

	flags.Int(option.NeighMapEntriesGlobalName, option.NATMapEntriesGlobalDefault, "Maximum number of entries for the global BPF neighbor table")
	option.BindEnv(vp, option.NeighMapEntriesGlobalName)

	flags.Duration(option.PolicyMapFullReconciliationIntervalName, 15*time.Minute, "Interval for full reconciliation of endpoint policy map")
	option.BindEnv(vp, option.PolicyMapFullReconciliationIntervalName)
	flags.MarkHidden(option.PolicyMapFullReconciliationIntervalName)

	flags.Float64(option.MapEntriesGlobalDynamicSizeRatioName, 0.0025, "Ratio (0.0-1.0] of total system memory to use for dynamic sizing of CT, NAT and policy BPF maps")
	option.BindEnv(vp, option.MapEntriesGlobalDynamicSizeRatioName)

	flags.String(option.CMDRef, "", "Path to cmdref output directory")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(vp, option.CMDRef)

	flags.Int(option.ToFQDNsMinTTL, defaults.ToFQDNsMinTTL, "The minimum time, in seconds, to use DNS data for toFQDNs policies")
	option.BindEnv(vp, option.ToFQDNsMinTTL)

	flags.Int(option.ToFQDNsProxyPort, 0, "Global port on which the in-agent DNS proxy should listen. Default 0 is a OS-assigned port.")
	option.BindEnv(vp, option.ToFQDNsProxyPort)

	flags.String(option.FQDNRejectResponseCode, option.FQDNProxyDenyWithRefused, fmt.Sprintf("DNS response code for rejecting DNS requests, available options are '%v'", option.FQDNRejectOptions))
	option.BindEnv(vp, option.FQDNRejectResponseCode)

	flags.Int(option.ToFQDNsMaxIPsPerHost, defaults.ToFQDNsMaxIPsPerHost, "Maximum number of IPs to maintain per FQDN name for each endpoint")
	option.BindEnv(vp, option.ToFQDNsMaxIPsPerHost)

	flags.Int(option.DNSMaxIPsPerRestoredRule, defaults.DNSMaxIPsPerRestoredRule, "Maximum number of IPs to maintain for each restored DNS rule")
	option.BindEnv(vp, option.DNSMaxIPsPerRestoredRule)

	flags.Bool(option.DNSPolicyUnloadOnShutdown, false, "Unload DNS policy rules on graceful shutdown")
	option.BindEnv(vp, option.DNSPolicyUnloadOnShutdown)

	flags.Int(option.ToFQDNsMaxDeferredConnectionDeletes, defaults.ToFQDNsMaxDeferredConnectionDeletes, "Maximum number of IPs to retain for expired DNS lookups with still-active connections")
	option.BindEnv(vp, option.ToFQDNsMaxDeferredConnectionDeletes)

	flags.Duration(option.ToFQDNsIdleConnectionGracePeriod, defaults.ToFQDNsIdleConnectionGracePeriod, "Time during which idle but previously active connections with expired DNS lookups are still considered alive (default 0s)")
	option.BindEnv(vp, option.ToFQDNsIdleConnectionGracePeriod)

	flags.Duration(option.FQDNProxyResponseMaxDelay, defaults.FQDNProxyResponseMaxDelay, "The maximum time the DNS proxy holds an allowed DNS response before sending it along. Responses are sent as soon as the datapath is updated with the new IP information.")
	option.BindEnv(vp, option.FQDNProxyResponseMaxDelay)

	flags.Uint(option.FQDNRegexCompileLRUSize, defaults.FQDNRegexCompileLRUSize, "Size of the FQDN regex compilation LRU. Useful for heavy but repeated DNS L7 rules with MatchName or MatchPattern")
	flags.MarkHidden(option.FQDNRegexCompileLRUSize)
	option.BindEnv(vp, option.FQDNRegexCompileLRUSize)

	flags.String(option.ToFQDNsPreCache, defaults.ToFQDNsPreCache, "DNS cache data at this path is preloaded on agent startup")
	option.BindEnv(vp, option.ToFQDNsPreCache)

	flags.Bool(option.ToFQDNsEnableDNSCompression, defaults.ToFQDNsEnableDNSCompression, "Allow the DNS proxy to compress responses to endpoints that are larger than 512 Bytes or the EDNS0 option, if present")
	option.BindEnv(vp, option.ToFQDNsEnableDNSCompression)

	flags.Int(option.DNSProxyConcurrencyLimit, 0, "Limit concurrency of DNS message processing")
	option.BindEnv(vp, option.DNSProxyConcurrencyLimit)

	flags.Duration(option.DNSProxyConcurrencyProcessingGracePeriod, 0, "Grace time to wait when DNS proxy concurrent limit has been reached during DNS message processing")
	option.BindEnv(vp, option.DNSProxyConcurrencyProcessingGracePeriod)

	flags.Int(option.DNSProxyLockCount, defaults.DNSProxyLockCount, "Array size containing mutexes which protect against parallel handling of DNS response names. Preferably use prime numbers")
	flags.MarkHidden(option.DNSProxyLockCount)
	option.BindEnv(vp, option.DNSProxyLockCount)

	flags.Duration(option.DNSProxyLockTimeout, defaults.DNSProxyLockTimeout, fmt.Sprintf("Timeout when acquiring the locks controlled by --%s", option.DNSProxyLockCount))
	flags.MarkHidden(option.DNSProxyLockTimeout)
	option.BindEnv(vp, option.DNSProxyLockTimeout)

	flags.Int(option.DNSProxySocketLingerTimeout, defaults.DNSProxySocketLingerTimeout, "Timeout (in seconds) when closing the connection between the DNS proxy and the upstream server. "+
		"If set to 0, the connection is closed immediately (with TCP RST). If set to -1, the connection is closed asynchronously in the background")
	option.BindEnv(vp, option.DNSProxySocketLingerTimeout)

	flags.Bool(option.DNSProxyEnableTransparentMode, defaults.DNSProxyEnableTransparentMode, "Enable DNS proxy transparent mode")
	option.BindEnv(vp, option.DNSProxyEnableTransparentMode)

	flags.Bool(option.DNSProxyInsecureSkipTransparentModeCheck, false, "Allows DNS proxy transparent mode to be disabled even if encryption is enabled. Enabling this flag and disabling DNS proxy transparent mode will cause proxied DNS traffic to leave the node unencrypted.")
	flags.MarkHidden(option.DNSProxyInsecureSkipTransparentModeCheck)
	option.BindEnv(vp, option.DNSProxyInsecureSkipTransparentModeCheck)

	flags.Int(option.EndpointQueueSize, defaults.EndpointQueueSize, "Size of EventQueue per-endpoint")
	option.BindEnv(vp, option.EndpointQueueSize)

	flags.Duration(option.PolicyTriggerInterval, defaults.PolicyTriggerInterval, "Time between triggers of policy updates (regenerations for all endpoints)")
	flags.MarkHidden(option.PolicyTriggerInterval)
	option.BindEnv(vp, option.PolicyTriggerInterval)

	flags.Bool(option.PolicyAuditModeArg, false, "Enable policy audit (non-drop) mode")
	option.BindEnv(vp, option.PolicyAuditModeArg)

	flags.Bool(option.PolicyAccountingArg, true, "Enable policy accounting")
	option.BindEnv(vp, option.PolicyAccountingArg)

	flags.Bool(option.EnableIPv4FragmentsTrackingName, defaults.EnableIPv4FragmentsTracking, "Enable IPv4 fragments tracking for L4-based lookups")
	option.BindEnv(vp, option.EnableIPv4FragmentsTrackingName)

	flags.Bool(option.EnableIPv6FragmentsTrackingName, defaults.EnableIPv6FragmentsTracking, "Enable IPv6 fragments tracking for L4-based lookups")
	option.BindEnv(vp, option.EnableIPv6FragmentsTrackingName)

	flags.Int(option.FragmentsMapEntriesName, defaults.FragmentsMapEntries, "Maximum number of entries in fragments tracking map")
	option.BindEnv(vp, option.FragmentsMapEntriesName)

	flags.Int(option.BPFEventsDefaultRateLimit, 0, fmt.Sprintf("Limit of average number of messages per second that can be written to BPF events map (if set, --%s value must also be specified). If both --%s and --%s are 0 or not specified, no limit is imposed.", option.BPFEventsDefaultBurstLimit, option.BPFEventsDefaultRateLimit, option.BPFEventsDefaultBurstLimit))
	flags.MarkHidden(option.BPFEventsDefaultRateLimit)
	option.BindEnv(vp, option.BPFEventsDefaultRateLimit)

	flags.Int(option.BPFEventsDefaultBurstLimit, 0, fmt.Sprintf("Maximum number of messages that can be written to BPF events map in 1 second (if set, --%s value must also be specified). If both --%s and --%s are 0 or not specified, no limit is imposed.", option.BPFEventsDefaultRateLimit, option.BPFEventsDefaultBurstLimit, option.BPFEventsDefaultRateLimit))
	flags.MarkHidden(option.BPFEventsDefaultBurstLimit)
	option.BindEnv(vp, option.BPFEventsDefaultBurstLimit)

	flags.String(option.LocalRouterIPv4, "", "Link-local IPv4 used for Cilium's router devices")
	option.BindEnv(vp, option.LocalRouterIPv4)

	flags.String(option.LocalRouterIPv6, "", "Link-local IPv6 used for Cilium's router devices")
	option.BindEnv(vp, option.LocalRouterIPv6)

	flags.Var(option.NewMapOptions(&option.Config.BPFMapEventBuffers, option.Config.BPFMapEventBuffersValidator), option.BPFMapEventBuffers, "Configuration for BPF map event buffers: (example: --bpf-map-event-buffers cilium_ipcache_v2=enabled_1024_1h)")
	flags.MarkHidden(option.BPFMapEventBuffers)

	flags.Bool(option.EgressMultiHomeIPRuleCompat, false,
		"Offset routing table IDs under ENI IPAM mode to avoid collisions with reserved table IDs. If false, the offset is performed (new scheme), otherwise, the old scheme stays in-place.")
	flags.MarkDeprecated(option.EgressMultiHomeIPRuleCompat, "The feature will be removed in v1.19")
	option.BindEnv(vp, option.EgressMultiHomeIPRuleCompat)

	flags.Bool(option.InstallUplinkRoutesForDelegatedIPAM, false,
		"Install ingress/egress routes through uplink on host for Pods when working with delegated IPAM plugin.")
	option.BindEnv(vp, option.InstallUplinkRoutesForDelegatedIPAM)

	flags.Bool(option.InstallNoConntrackIptRules, defaults.InstallNoConntrackIptRules, "Install Iptables rules to skip netfilter connection tracking on all pod traffic. This option is only effective when Cilium is running in direct routing and full KPR mode. Moreover, this option cannot be enabled when Cilium is running in a managed Kubernetes environment or in a chained CNI setup.")
	option.BindEnv(vp, option.InstallNoConntrackIptRules)

	flags.String(option.ContainerIPLocalReservedPorts, defaults.ContainerIPLocalReservedPortsAuto, "Instructs the Cilium CNI plugin to reserve the provided comma-separated list of ports in the container network namespace. "+
		"Prevents the container from using these ports as ephemeral source ports (see Linux ip_local_reserved_ports). Use this flag if you observe port conflicts between transparent DNS proxy requests and host network namespace services. "+
		"Value \"auto\" reserves the WireGuard and VXLAN ports used by Cilium")
	option.BindEnv(vp, option.ContainerIPLocalReservedPorts)

	flags.Bool(option.EnableCustomCallsName, false, "Enable tail call hooks for custom eBPF programs")
	option.BindEnv(vp, option.EnableCustomCallsName)
	flags.MarkDeprecated(option.EnableCustomCallsName, "The feature has been deprecated and it will be removed in v1.19")

	// flags.IntSlice cannot be used due to missing support for appropriate conversion in Viper.
	// See https://github.com/cilium/cilium/pull/20282 for more information.
	flags.StringSlice(option.VLANBPFBypass, []string{}, "List of explicitly allowed VLAN IDs, '0' id will allow all VLAN IDs")
	option.BindEnv(vp, option.VLANBPFBypass)

	flags.Bool(option.DisableExternalIPMitigation, false, "Disable ExternalIP mitigation (CVE-2020-8554, default false)")
	option.BindEnv(vp, option.DisableExternalIPMitigation)

	flags.Bool(option.EnableICMPRules, defaults.EnableICMPRules, "Enable ICMP-based rule support for Cilium Network Policies")
	flags.MarkHidden(option.EnableICMPRules)
	option.BindEnv(vp, option.EnableICMPRules)

	flags.Bool(option.UseCiliumInternalIPForIPsec, defaults.UseCiliumInternalIPForIPsec, "Use the CiliumInternalIPs (vs. NodeInternalIPs) for IPsec encapsulation")
	flags.MarkHidden(option.UseCiliumInternalIPForIPsec)
	option.BindEnv(vp, option.UseCiliumInternalIPForIPsec)

	flags.Bool(option.BypassIPAvailabilityUponRestore, false, "Bypasses the IP availability error within IPAM upon endpoint restore")
	flags.MarkHidden(option.BypassIPAvailabilityUponRestore)
	option.BindEnv(vp, option.BypassIPAvailabilityUponRestore)

	flags.Bool(option.EnableCiliumEndpointSlice, false, "Enable the CiliumEndpointSlice watcher in place of the CiliumEndpoint watcher (beta)")
	option.BindEnv(vp, option.EnableCiliumEndpointSlice)

	flags.Bool(option.EnableVTEP, defaults.EnableVTEP, "Enable  VXLAN Tunnel Endpoint (VTEP) Integration (beta)")
	option.BindEnv(vp, option.EnableVTEP)

	flags.StringSlice(option.VtepEndpoint, []string{}, "List of VTEP IP addresses")
	option.BindEnv(vp, option.VtepEndpoint)

	flags.StringSlice(option.VtepCIDR, []string{}, "List of VTEP CIDRs that will be routed towards VTEPs for traffic cluster egress")
	option.BindEnv(vp, option.VtepCIDR)

	flags.String(option.VtepMask, "255.255.255.0", "VTEP CIDR Mask for all VTEP CIDRs")
	option.BindEnv(vp, option.VtepMask)

	flags.StringSlice(option.VtepMAC, []string{}, "List of VTEP MAC addresses for forwarding traffic outside the cluster")
	option.BindEnv(vp, option.VtepMAC)

	flags.Int(option.TCFilterPriority, 1, "Priority of TC BPF filter")
	flags.MarkHidden(option.TCFilterPriority)
	option.BindEnv(vp, option.TCFilterPriority)

	flags.Bool(option.EnableBGPControlPlane, false, "Enable the BGP control plane.")
	option.BindEnv(vp, option.EnableBGPControlPlane)

	flags.Bool(option.EnableBGPControlPlaneStatusReport, true, "Enable the BGP control plane status reporting")
	option.BindEnv(vp, option.EnableBGPControlPlaneStatusReport)

	flags.String(option.BGPRouterIDAllocationMode, option.BGPRouterIDAllocationModeDefault, "BGP router-id allocation mode. Currently supported values: 'default' or 'ip-pool'")
	option.BindEnv(vp, option.BGPRouterIDAllocationMode)

	flags.String(option.BGPRouterIDAllocationIPPool, "", "IP pool to allocate the BGP router-id from when the mode is 'ip-pool'")
	option.BindEnv(vp, option.BGPRouterIDAllocationIPPool)

	flags.Bool(option.EnablePMTUDiscovery, false, "Enable path MTU discovery to send ICMP fragmentation-needed replies to the client")
	option.BindEnv(vp, option.EnablePMTUDiscovery)

	flags.Duration(option.IPAMCiliumNodeUpdateRate, 15*time.Second, "Maximum rate at which the CiliumNode custom resource is updated")
	option.BindEnv(vp, option.IPAMCiliumNodeUpdateRate)

	flags.Bool(option.EnableK8sNetworkPolicy, defaults.EnableK8sNetworkPolicy, "Enable support for K8s NetworkPolicy")
	flags.MarkHidden(option.EnableK8sNetworkPolicy)
	option.BindEnv(vp, option.EnableK8sNetworkPolicy)

	flags.Bool(option.EnableCiliumNetworkPolicy, defaults.EnableCiliumNetworkPolicy, "Enable support for Cilium Network Policy")
	flags.MarkHidden(option.EnableCiliumNetworkPolicy)
	option.BindEnv(vp, option.EnableCiliumNetworkPolicy)

	flags.Bool(option.EnableCiliumClusterwideNetworkPolicy, defaults.EnableCiliumClusterwideNetworkPolicy, "Enable support for Cilium Clusterwide Network Policy")
	flags.MarkHidden(option.EnableCiliumClusterwideNetworkPolicy)
	option.BindEnv(vp, option.EnableCiliumClusterwideNetworkPolicy)

	flags.StringSlice(option.PolicyCIDRMatchMode, defaults.PolicyCIDRMatchMode, "The entities that can be selected by CIDR policy. Supported values: 'nodes'")
	option.BindEnv(vp, option.PolicyCIDRMatchMode)

	flags.Duration(option.MaxInternalTimerDelay, defaults.MaxInternalTimerDelay, "Maximum internal timer value across the entire agent. Use in test environments to detect race conditions in agent logic.")
	flags.MarkHidden(option.MaxInternalTimerDelay)
	option.BindEnv(vp, option.MaxInternalTimerDelay)

	flags.Bool(option.EnableNodeSelectorLabels, defaults.EnableNodeSelectorLabels, "Enable use of node label based identity")
	option.BindEnv(vp, option.EnableNodeSelectorLabels)

	flags.StringSlice(option.NodeLabels, []string{}, "List of label prefixes used to determine identity of a node (used only when enable-node-selector-labels is enabled)")
	option.BindEnv(vp, option.NodeLabels)

	flags.Bool(option.EnableInternalTrafficPolicy, defaults.EnableInternalTrafficPolicy, "Enable internal traffic policy")
	flags.MarkDeprecated(option.EnableInternalTrafficPolicy, "The flag will be removed in v1.19. The feature will be unconditionally enabled by default.")
	option.BindEnv(vp, option.EnableInternalTrafficPolicy)

	flags.Bool(option.EnableNonDefaultDenyPolicies, defaults.EnableNonDefaultDenyPolicies, "Enable use of non-default-deny policies")
	flags.MarkHidden(option.EnableNonDefaultDenyPolicies)
	option.BindEnv(vp, option.EnableNonDefaultDenyPolicies)

	flags.Bool(option.WireguardTrackAllIPsFallback, defaults.WireguardTrackAllIPsFallback, "Force WireGuard to track all IPs")
	flags.MarkHidden(option.WireguardTrackAllIPsFallback)
	option.BindEnv(vp, option.WireguardTrackAllIPsFallback)

	flags.Bool(option.EnableEndpointLockdownOnPolicyOverflow, false, "When an endpoint's policy map overflows, shutdown all (ingress and egress) network traffic for that endpoint.")
	option.BindEnv(vp, option.EnableEndpointLockdownOnPolicyOverflow)

	flags.String(option.BootIDFilename, "/proc/sys/kernel/random/boot_id", "Path to filename of the boot ID")
	flags.MarkHidden(option.BootIDFilename)
	option.BindEnv(vp, option.BootIDFilename)

	flags.Float64(option.ConnectivityProbeFrequencyRatio, defaults.ConnectivityProbeFrequencyRatio, "Ratio of the connectivity probe frequency vs resource usage, a float in [0, 1]. 0 will give more frequent probing, 1 will give less frequent probing. Probing frequency is dynamically adjusted based on the cluster size.")
	option.BindEnv(vp, option.ConnectivityProbeFrequencyRatio)

	flags.Bool(option.EnableExtendedIPProtocols, defaults.EnableExtendedIPProtocols, "Enable traffic with extended IP protocols in datapath")
	option.BindEnv(vp, option.EnableExtendedIPProtocols)

	if err := vp.BindPFlags(flags); err != nil {
		logging.Fatal(logger, "BindPFlags failed", logfields.Error, err)
	}
}

// restoreExecPermissions restores file permissions to 0740 of all files inside
// `searchDir` with the given regex `patterns`.
func restoreExecPermissions(searchDir string, patterns ...string) error {
	fileList := []string{}
	err := filepath.Walk(searchDir, func(path string, f os.FileInfo, err error) error {
		for _, pattern := range patterns {
			if regexp.MustCompile(pattern).MatchString(f.Name()) {
				fileList = append(fileList, path)
				break
			}
		}
		return nil
	})
	for _, fileToChange := range fileList {
		// Changing files permissions to -rwx:r--:---, we are only
		// adding executable permission to the owner and keeping the
		// same permissions stored by go-bindata.
		if err := os.Chmod(fileToChange, os.FileMode(0740)); err != nil {
			return err
		}
	}
	return err
}

func initDaemonConfigAndLogging(vp *viper.Viper) {
	option.Config.SetMapElementSizes(
		// for the conntrack and NAT element size we assume the largest possible
		// key size, i.e. IPv6 keys
		ctmap.SizeofCtKey6Global+ctmap.SizeofCtEntry,
		nat.SizeofNatKey6+nat.SizeofNatEntry6,
		neighborsmap.SizeofNeighKey6+neighborsmap.SizeOfNeighValue,
		lbmaps.SizeofSockRevNat6Key+lbmaps.SizeofSockRevNat6Value)

	option.Config.SetupLogging(vp, "cilium-agent")

	// slogloggercheck: using default logger for configuration initialization
	option.Config.Populate(logging.DefaultSlogLogger, vp)

	// add hooks after setting up metrics in the option.Config
	logging.AddHandlers(metrics.NewLoggingHook())

	time.MaxInternalTimerDelay = vp.GetDuration(option.MaxInternalTimerDelay)
}

func initEnv(logger *slog.Logger, vp *viper.Viper) {
	bootstrapStats.earlyInit.Start()
	defer bootstrapStats.earlyInit.End(true)

	var debugDatapath bool

	option.LogRegisteredSlogOptions(vp, logger)

	for _, grp := range option.Config.DebugVerbose {
		switch grp {
		case argDebugVerboseFlow:
			logger.Debug("Enabling flow debug")
			flowdebug.Enable()
		case argDebugVerboseKvstore:
			kvstore.EnableTracing()
		case argDebugVerboseEnvoy:
			logger.Debug("Enabling Envoy tracing")
			envoy.EnableTracing()
		case argDebugVerboseDatapath:
			logger.Debug("Enabling datapath debug messages")
			debugDatapath = true
		case argDebugVerbosePolicy:
			option.Config.Opts.SetBool(option.DebugPolicy, true)
		default:
			logger.Warn("Unknown verbose debug group", logfields.Group, grp)
		}
	}
	// Enable policy debugging if debug is enabled.
	if option.Config.Debug {
		option.Config.Opts.SetBool(option.DebugPolicy, true)
	}

	common.RequireRootPrivilege("cilium-agent")

	logger.Info("     _ _ _")
	logger.Info(" ___|_| |_|_ _ _____")
	logger.Info("|  _| | | | | |     |")
	logger.Info("|___|_|_|_|___|_|_|_|")
	logger.Info(fmt.Sprintf("Cilium %s", version.Version))

	if option.Config.LogSystemLoadConfig {
		loadinfo.StartBackgroundLogger(logger)
	}

	if option.Config.PreAllocateMaps {
		bpf.EnableMapPreAllocation()
	}
	if option.Config.BPFDistributedLRU {
		bpf.EnableMapDistributedLRU()
	}

	option.Config.BpfDir = filepath.Join(option.Config.LibDir, defaults.BpfDir)
	option.Config.StateDir = filepath.Join(option.Config.RunDir, defaults.StateDir)

	scopedLog := logger.With(
		logfields.RunDirectory, option.Config.RunDir,
		logfields.LibDirectory, option.Config.LibDir,
		logfields.BPFDirectory, option.Config.BpfDir,
		logfields.StateDirectory, option.Config.StateDir,
	)

	if err := os.MkdirAll(option.Config.RunDir, defaults.RuntimePathRights); err != nil {
		logging.Fatal(scopedLog, "Could not create runtime directory", logfields.Error, err)
	}

	if option.Config.RunDir != defaults.RuntimePath {
		if err := os.MkdirAll(defaults.RuntimePath, defaults.RuntimePathRights); err != nil {
			logging.Fatal(scopedLog, "Could not create default runtime directory", logfields.Error, err)
		}
	}

	if err := os.MkdirAll(option.Config.StateDir, defaults.StateDirRights); err != nil {
		logging.Fatal(scopedLog, "Could not create state directory", logfields.Error, err)
	}

	if err := os.MkdirAll(option.Config.LibDir, defaults.RuntimePathRights); err != nil {
		logging.Fatal(scopedLog, "Could not create library directory", logfields.Error, err)
	}
	// Restore permissions of executable files
	if err := restoreExecPermissions(option.Config.LibDir, `.*\.sh`); err != nil {
		logging.Fatal(scopedLog, "Unable to restore agent asset permissions", logfields.Error, err)
	}

	// Creating Envoy sockets directory for cases which doesn't provide a volume mount
	// (e.g. embedded Envoy, external workload in ClusterMesh scenario)
	if err := os.MkdirAll(envoy.GetSocketDir(option.Config.RunDir), defaults.RuntimePathRights); err != nil {
		logging.Fatal(scopedLog, "Could not create envoy sockets directory", logfields.Error, err)
	}

	// set rlimit Memlock to INFINITY before creating any bpf resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		logging.Fatal(scopedLog, "unable to set memory resource limits", logfields.Error, err)
	}

	globalsDir := option.Config.GetGlobalsDir()
	if err := os.MkdirAll(globalsDir, defaults.StateDirRights); err != nil {
		logging.Fatal(scopedLog, "Could not create runtime directory",
			logfields.Error, err,
			logfields.Path, globalsDir,
		)
	}
	if err := os.Chdir(option.Config.StateDir); err != nil {
		logging.Fatal(scopedLog, "Could not change to runtime directory",
			logfields.Error, err,
			logfields.Path, option.Config.StateDir,
		)
	}
	if _, err := os.Stat(option.Config.BpfDir); os.IsNotExist(err) {
		logging.Fatal(scopedLog, "BPF template directory: NOT OK. Please run 'make install-bpf'", logfields.Error, err)
	}

	if err := probes.CreateHeaderFiles(filepath.Join(option.Config.BpfDir, "include/bpf"), probes.ExecuteHeaderProbes(scopedLog)); err != nil {
		logging.Fatal(scopedLog, "failed to create header files with feature macros", logfields.Error, err)
	}

	if err := pidfile.Write(defaults.PidFilePath); err != nil {
		logging.Fatal(scopedLog, "Failed to create Pidfile",
			logfields.Error, err,
			logfields.Path, defaults.PidFilePath,
		)
	}

	option.Config.AllowLocalhost = strings.ToLower(option.Config.AllowLocalhost)
	switch option.Config.AllowLocalhost {
	case option.AllowLocalhostAlways, option.AllowLocalhostAuto, option.AllowLocalhostPolicy:
	default:
		logging.Fatal(scopedLog, fmt.Sprintf("Invalid setting for --allow-localhost, must be { %s, %s, %s }",
			option.AllowLocalhostAuto, option.AllowLocalhostAlways, option.AllowLocalhostPolicy))
	}

	scopedLog = logger.With(logfields.Path, option.Config.SocketPath)
	socketDir := path.Dir(option.Config.SocketPath)
	if err := os.MkdirAll(socketDir, defaults.RuntimePathRights); err != nil {
		logging.Fatal(
			scopedLog,
			"Cannot mkdir directory for cilium socket",
			logfields.Error, err,
		)
	}

	if err := os.Remove(option.Config.SocketPath); !os.IsNotExist(err) && err != nil {
		logging.Fatal(
			scopedLog,
			"Cannot remove existing Cilium sock",
			logfields.Error, err,
		)
	}

	// The standard operation is to mount the BPF filesystem to the
	// standard location (/sys/fs/bpf). The user may choose to specify
	// the path to an already mounted filesystem instead. This is
	// useful if the daemon is being round inside a namespace and the
	// BPF filesystem is mapped into the slave namespace.
	bpf.CheckOrMountFS(logger, option.Config.BPFRoot)
	cgroups.CheckOrMountCgrpFS(logger, option.Config.CGroupRoot)

	option.Config.Opts.SetBool(option.Debug, debugDatapath)
	option.Config.Opts.SetBool(option.DebugLB, debugDatapath)
	option.Config.Opts.SetBool(option.DropNotify, option.Config.BPFEventsDropEnabled)
	option.Config.Opts.SetBool(option.PolicyVerdictNotify, option.Config.BPFEventsPolicyVerdictEnabled)
	option.Config.Opts.SetBool(option.TraceNotify, option.Config.BPFEventsTraceEnabled)
	option.Config.Opts.SetBool(option.PolicyTracing, option.Config.EnableTracing)
	option.Config.Opts.SetBool(option.ConntrackAccounting, option.Config.BPFConntrackAccounting)
	option.Config.Opts.SetBool(option.PolicyAuditMode, option.Config.PolicyAuditMode)
	option.Config.Opts.SetBool(option.PolicyAccounting, option.Config.PolicyAccounting)
	option.Config.Opts.SetBool(option.SourceIPVerification, option.Config.EnableSourceIPVerification)

	monitorAggregationLevel, err := option.ParseMonitorAggregationLevel(option.Config.MonitorAggregation)
	if err != nil {
		logging.Fatal(logger, fmt.Sprintf("Failed to parse %s", option.MonitorAggregationName), logfields.Error, err)
	}
	option.Config.Opts.SetValidated(option.MonitorAggregation, monitorAggregationLevel)

	policy.SetPolicyEnabled(option.Config.EnablePolicy)
	if option.Config.PolicyAuditMode {
		logger.Warn(fmt.Sprintf("%s is enabled. Network policy will not be enforced.", option.PolicyAuditMode))
	}

	if err := identity.AddUserDefinedNumericIdentitySet(option.Config.FixedIdentityMapping); err != nil {
		logging.Fatal(logger, "Invalid fixed identities provided", logfields.Error, err)
	}

	if !option.Config.EnableIPv4 && !option.Config.EnableIPv6 {
		logging.Fatal(logger, "Either IPv4 or IPv6 addressing must be enabled")
	}
	if err := labelsfilter.ParseLabelPrefixCfg(logger, option.Config.Labels, option.Config.NodeLabels, option.Config.LabelPrefixFile); err != nil {
		logging.Fatal(logger, "Unable to parse Label prefix configuration", logfields.Error, err)
	}

	switch option.Config.DatapathMode {
	case datapathOption.DatapathModeVeth:
	case datapathOption.DatapathModeNetkit, datapathOption.DatapathModeNetkitL2:
		// For netkit we enable also tcx for all non-netkit devices.
		// The underlying kernel does support it given tcx got merged
		// before netkit and supporting legacy tc in this context does
		// not make any sense whatsoever.
		option.Config.EnableTCX = true
		if err := probes.HaveNetkit(); err != nil {
			logging.Fatal(logger, "netkit devices need kernel 6.7.0 or newer and CONFIG_NETKIT")
		}
	default:
		logging.Fatal(logger, "Invalid datapath mode", logfields.DatapathMode, option.Config.DatapathMode)
	}

	if option.Config.EnableL7Proxy && !option.Config.InstallIptRules {
		logging.Fatal(logger, "L7 proxy requires iptables rules (--install-iptables-rules=\"true\")")
	}

	if !option.Config.DNSProxyInsecureSkipTransparentModeCheck {
		if option.Config.EnableIPSec && option.Config.EnableL7Proxy && !option.Config.DNSProxyEnableTransparentMode {
			logging.Fatal(logger, "IPSec requires DNS proxy transparent mode to be enabled (--dnsproxy-enable-transparent-mode=\"true\")")
		}
	}

	if option.Config.EnableIPSec && option.Config.TunnelingEnabled() {
		if err := ipsec.ProbeXfrmStateOutputMask(); err != nil {
			logging.Fatal(logger, "IPSec with tunneling requires support for xfrm state output masks (Linux 4.19 or later).", logfields.Error, err)
		}
	}

	if option.Config.EnableIPSecEncryptedOverlay && !option.Config.EnableIPSec {
		logger.Warn("IPSec encrypted overlay is enabled but IPSec is not. Ignoring option.")
	}

	if option.Config.TunnelingEnabled() && option.Config.EnableAutoDirectRouting {
		logging.Fatal(logger, fmt.Sprintf("%s cannot be used with tunneling. Packets must be routed through the tunnel device.", option.EnableAutoDirectRoutingName))
	}

	initClockSourceOption(logger)

	if option.Config.EnableSRv6 {
		if !option.Config.EnableIPv6 {
			logging.Fatal(logger, "SRv6 requires IPv6.")
		}
	}

	if option.Config.EnableHostFirewall {
		if option.Config.EnableIPSec {
			logging.Fatal(logger, "IPSec cannot be used with the host firewall.")
		}
	}

	if option.Config.EnableIPv4FragmentsTracking {
		if !option.Config.EnableIPv4 {
			option.Config.EnableIPv4FragmentsTracking = false
		}
	}

	if option.Config.EnableIPv6FragmentsTracking {
		if !option.Config.EnableIPv6 {
			option.Config.EnableIPv6FragmentsTracking = false
		}
	}

	if option.Config.EnableBPFTProxy {
		if probes.HaveProgramHelper(logger, ebpf.SchedCLS, asm.FnSkAssign) != nil {
			option.Config.EnableBPFTProxy = false
			logger.Info("Disabled support for BPF TProxy due to missing kernel support for socket assign (Linux 5.7 or later)")
		}
	}

	if option.Config.LocalRouterIPv4 != "" || option.Config.LocalRouterIPv6 != "" {
		// TODO(weil0ng): add a proper check for ipam in PR# 15429.
		if option.Config.TunnelingEnabled() {
			logging.Fatal(logger, fmt.Sprintf("Cannot specify %s or %s in tunnel mode.", option.LocalRouterIPv4, option.LocalRouterIPv6))
		}
		if !option.Config.EnableEndpointRoutes {
			logging.Fatal(logger, fmt.Sprintf("Cannot specify %s or %s  without %s.", option.LocalRouterIPv4, option.LocalRouterIPv6, option.EnableEndpointRoutes))
		}
		if option.Config.EnableIPSec {
			logging.Fatal(logger, fmt.Sprintf("Cannot specify %s or %s with %s.", option.LocalRouterIPv4, option.LocalRouterIPv6, option.EnableIPSecName))
		}
	}

	if option.Config.EnableEndpointRoutes && option.Config.EnableLocalNodeRoute {
		option.Config.EnableLocalNodeRoute = false
		logger.Debug(
			"Auto-set option to `false` because it is redundant to per-endpoint routes",
			logfields.Option, option.EnableLocalNodeRoute,
			option.EnableEndpointRoutes, true,
		)
	}

	if option.Config.IPAM == ipamOption.IPAMAzure {
		option.Config.EgressMultiHomeIPRuleCompat = true
		logger.Debug(
			fmt.Sprintf("Auto-set %q to `true` because the Azure datapath has not been migrated over to a new scheme. "+
				"A future version of Cilium will support a newer Azure datapath. "+
				"Connectivity is not affected.",
				option.EgressMultiHomeIPRuleCompat),
			logfields.URL, "https://github.com/cilium/cilium/issues/14705",
		)
	}

	if option.Config.IPAM == ipamOption.IPAMENI && option.Config.TunnelingEnabled() {
		logging.Fatal(logger, fmt.Sprintf("Cannot specify IPAM mode %s in tunnel mode.", option.Config.IPAM))
	}

	if option.Config.InstallNoConntrackIptRules {
		// InstallNoConntrackIptRules can only be enabled in direct
		// routing mode as in tunneling mode the encapsulated traffic is
		// already skipping netfilter conntrack.
		if option.Config.TunnelingEnabled() {
			logging.Fatal(logger, fmt.Sprintf("%s requires the agent to run in direct routing mode.", option.InstallNoConntrackIptRules))
		}

		// Moreover InstallNoConntrackIptRules requires IPv4 support as
		// the native routing CIDR, used to select all pod traffic, can
		// only be an IPv4 CIDR at the moment.
		if !option.Config.EnableIPv4 {
			logging.Fatal(logger, fmt.Sprintf("%s requires IPv4 support.", option.InstallNoConntrackIptRules))
		}
	}

	// Ensure that the user does not turn on this mode unless it's for an IPAM
	// mode which support the bypass.
	if option.Config.BypassIPAvailabilityUponRestore {
		switch option.Config.IPAMMode() {
		case ipamOption.IPAMENI, ipamOption.IPAMAzure:
			logger.Info(
				"Running with bypass of IP not available errors upon endpoint " +
					"restore. Be advised that this mode is intended to be " +
					"temporary to ease upgrades. Consider restarting the pods " +
					"which have IPs not from the pool.",
			)
		default:
			option.Config.BypassIPAvailabilityUponRestore = false
			logger.Warn(
				fmt.Sprintf(
					"Bypassing IP allocation upon endpoint restore (%q) is enabled with"+
						"unintended IPAM modes. This bypass is only intended "+
						"to work for CRD-based IPAM modes such as ENI. Disabling "+
						"bypass.",
					option.BypassIPAvailabilityUponRestore,
				),
			)
		}
	}
}

// daemonCell wraps the existing implementation of the cilium-agent that has
// not yet been converted into a cell. Provides *Daemon as a Promise that is
// resolved once daemon has been started to facilitate conversion into modules.
var daemonCell = cell.Module(
	"daemon",
	"Legacy Daemon",

	cell.Provide(
		newDaemonPromise,
		promise.New[endpointstate.Restorer],
		promise.New[*option.DaemonConfig],
		newSyncHostIPs,
	),
	cell.Invoke(registerEndpointStateResolver),
	cell.Invoke(func(promise.Promise[*Daemon]) {}), // Force instantiation.
)

type daemonParams struct {
	cell.In

	CfgResolver promise.Resolver[*option.DaemonConfig]

	Logger              *slog.Logger
	Lifecycle           cell.Lifecycle
	Health              cell.Health
	MetricsRegistry     *metrics.Registry
	Clientset           k8sClient.Clientset
	KVStoreClient       kvstore.Client
	WGAgent             *wireguard.Agent
	LocalNodeStore      *node.LocalNodeStore
	Shutdowner          hive.Shutdowner
	Resources           agentK8s.Resources
	K8sWatcher          *watchers.K8sWatcher
	CacheStatus         k8sSynced.CacheStatus
	K8sResourceSynced   *k8sSynced.Resources
	K8sAPIGroups        *k8sSynced.APIGroups
	NodeManager         nodeManager.NodeManager
	NodeHandler         datapath.NodeHandler
	NodeAddressing      datapath.NodeAddressing
	EndpointCreator     endpointcreator.EndpointCreator
	EndpointManager     endpointmanager.EndpointManager
	EndpointMetadata    endpointmetadata.EndpointMetadataFetcher
	CertManager         certificatemanager.CertificateManager
	SecretManager       certificatemanager.SecretManager
	IdentityAllocator   identitycell.CachingIdentityAllocator
	IdentityRestorer    *identityrestoration.LocalIdentityRestorer
	JobGroup            job.Group
	Policy              policy.PolicyRepository
	IPCache             *ipcache.IPCache
	DirReadStatus       policyDirectory.DirectoryWatcherReadStatus
	CiliumHealth        health.CiliumHealthManager
	ClusterMesh         *clustermesh.ClusterMesh
	MonitorAgent        monitorAgent.Agent
	DB                  *statedb.DB
	Namespaces          statedb.Table[agentK8s.Namespace]
	Routes              statedb.Table[*datapathTables.Route]
	Devices             statedb.Table[*datapathTables.Device]
	NodeAddrs           statedb.Table[datapathTables.NodeAddress]
	DirectRoutingDevice datapathTables.DirectRoutingDevice
	// Grab the GC object so that we can start the CT/NAT map garbage collection.
	// This is currently necessary because these maps have not yet been modularized,
	// and because it depends on parameters which are not provided through hive.
	CTNATMapGC          ctmap.GCRunner
	IPIdentityWatcher   *ipcache.LocalIPIdentityWatcher
	EndpointRegenerator *endpoint.Regenerator
	ClusterInfo         cmtypes.ClusterInfo
	TunnelConfig        tunnel.Config
	BandwidthManager    datapath.BandwidthManager
	IPsecKeyCustodian   datapath.IPsecKeyCustodian
	MTU                 mtu.MTU
	Sysctl              sysctl.Sysctl
	SyncHostIPs         *syncHostIPs
	NodeDiscovery       *nodediscovery.NodeDiscovery
	IPAM                *ipam.IPAM
	CRDSyncPromise      promise.Promise[k8sSynced.CRDSync]
	IdentityManager     identitymanager.IDManager
	MaglevConfig        maglev.Config
	LBConfig            loadbalancer.Config
	DNSProxy            bootstrap.FQDNProxyBootstrapper
	DNSNameManager      namemanager.NameManager
	KPRConfig           kpr.KPRConfig
}

func newDaemonPromise(params daemonParams) (promise.Promise[*Daemon], legacy.DaemonInitialization) {
	daemonResolver, daemonPromise := promise.New[*Daemon]()

	// daemonCtx is the daemon-wide context cancelled when stopping.
	daemonCtx, cancelDaemonCtx := context.WithCancel(context.Background())
	cleaner := NewDaemonCleanup()

	var daemon *Daemon
	var wg sync.WaitGroup

	params.Lifecycle.Append(cell.Hook{
		OnStart: func(cell.HookContext) (err error) {
			defer func() {
				// Reject promises on error
				if err != nil {
					params.CfgResolver.Reject(err)
					daemonResolver.Reject(err)
				}
			}()

			d, restoredEndpoints, err := newDaemon(daemonCtx, cleaner, &params)
			if err != nil {
				cancelDaemonCtx()
				cleaner.Clean()
				return fmt.Errorf("daemon creation failed: %w", err)
			}
			daemon = d

			if !option.Config.DryMode {
				d.logger.Info("Initializing daemon")

				// This validation needs to be done outside of the agent until
				// datapath.NodeAddressing is used consistently across the code base.
				d.logger.Info("Validating configured node address ranges")
				if err := node.ValidatePostInit(params.Logger); err != nil {
					return fmt.Errorf("postinit failed: %w", err)
				}

				// Store config in file before resolving the DaemonConfig promise.
				err = option.Config.StoreInFile(d.logger, option.Config.StateDir)
				if err != nil {
					d.logger.Error("Unable to store Cilium's configuration", logfields.Error, err)
				}

				err = option.StoreViperInFile(d.logger, option.Config.StateDir)
				if err != nil {
					d.logger.Error("Unable to store Viper's configuration", logfields.Error, err)
				}
			}

			// 'option.Config' is assumed to be stable at this point, execpt for
			// 'option.Config.Opts' that are explicitly deemed to be runtime-changeable
			params.CfgResolver.Resolve(option.Config)

			if option.Config.DryMode {
				daemonResolver.Resolve(daemon)
			} else {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := startDaemon(daemon, restoredEndpoints, cleaner, params); err != nil {
						d.logger.Error("Daemon start failed", logfields.Error, err)
						daemonResolver.Reject(err)
					} else {
						daemonResolver.Resolve(daemon)
					}
				}()
			}
			return nil
		},
		OnStop: func(cell.HookContext) error {
			cancelDaemonCtx()
			cleaner.Clean()
			wg.Wait()
			return nil
		},
	})
	return daemonPromise, legacy.DaemonInitialization{}
}

// startDaemon starts the old unmodular part of the cilium-agent.
// option.Config has already been exposed via *option.DaemonConfig promise,
// so it may not be modified here
func startDaemon(d *Daemon, restoredEndpoints *endpointRestoreState, cleaner *daemonCleanup, params daemonParams) error {
	bootstrapStats.k8sInit.Start()
	if params.Clientset.IsEnabled() {
		// Wait only for certain caches, but not all!
		// (Check Daemon.InitK8sSubsystem() for more info)
		select {
		case <-params.CacheStatus:
		case <-d.ctx.Done():
			return d.ctx.Err()
		}
	}

	// wait for directory watcher to ingest policy from files
	params.DirReadStatus.Wait()

	bootstrapStats.k8sInit.End(true)

	// After K8s caches have been synced, IPCache can start label injection.
	// Ensure that the initial labels are injected before we regenerate endpoints
	d.logger.Debug("Waiting for initial IPCache revision")
	if err := d.ipcache.WaitForRevision(d.ctx, 1); err != nil {
		d.logger.Error("Failed to wait for initial IPCache revision", logfields.Error, err)
	}

	d.initRestore(restoredEndpoints, params.EndpointRegenerator)

	bootstrapStats.enableConntrack.Start()
	d.logger.Info("Starting connection tracking garbage collector")
	params.CTNATMapGC.Enable()
	params.CTNATMapGC.Observe4().Observe(d.ctx, ctmap.NatMapNext4, func(err error) {})
	params.CTNATMapGC.Observe6().Observe(d.ctx, ctmap.NatMapNext6, func(err error) {})
	bootstrapStats.enableConntrack.End(true)

	if params.WGAgent != nil {
		go func() {
			// Wait until the kvstore synchronization completed, to avoid
			// causing connectivity blips due incorrectly removing
			// WireGuard peers that have not yet been discovered.
			// WaitForKVStoreSync returns immediately in CRD mode.
			if err := d.nodeDiscovery.WaitForKVStoreSync(d.ctx); err != nil {
				return
			}

			// When running in KVStore mode, we need to additionally wait until
			// we have discovered all remote IP addresses, to prevent triggering
			// the collection of stale AllowedIPs entries too early, leading to
			// the disruption of otherwise valid long running connections.
			if err := params.IPIdentityWatcher.WaitForSync(d.ctx); err != nil {
				return
			}

			if err := params.WGAgent.RestoreFinished(d.clustermesh); err != nil {
				d.logger.Error("Failed to set up WireGuard peers", logfields.Error, err)
			}
		}()
	}

	if d.endpointManager.HostEndpointExists() {
		d.endpointManager.InitHostEndpointLabels(d.ctx)
	} else {
		d.logger.Info("Creating host endpoint")
		if err := d.endpointCreator.AddHostEndpoint(d.ctx); err != nil {
			return fmt.Errorf("unable to create host endpoint: %w", err)
		}
	}

	if option.Config.EnableEnvoyConfig {
		if !d.endpointManager.IngressEndpointExists() {
			// Creating Ingress Endpoint depends on the Ingress IPs having been
			// allocated first. This happens earlier in the agent bootstrap.
			if (option.Config.EnableIPv4 && len(node.GetIngressIPv4(params.Logger)) == 0) ||
				(option.Config.EnableIPv6 && len(node.GetIngressIPv6(params.Logger)) == 0) {
				d.logger.Warn("Ingress IPs are not available, skipping creation of the Ingress Endpoint: Policy enforcement on Cilium Ingress will not work as expected.")
			} else {
				d.logger.Info("Creating ingress endpoint")
				err := d.endpointCreator.AddIngressEndpoint(d.ctx)
				if err != nil {
					return fmt.Errorf("unable to create ingress endpoint: %w", err)
				}
			}
		}
	}

	go func() {
		if d.endpointRestoreComplete != nil {
			select {
			case <-d.endpointRestoreComplete:
			case <-d.ctx.Done():
				return
			}
		}

		ms := maps.NewMapSweeper(
			d.logger,
			&EndpointMapManager{
				logger:          d.logger,
				EndpointManager: d.endpointManager,
			}, d.bwManager, d.lbConfig, d.kprCfg)
		ms.CollectStaleMapGarbage()
		ms.RemoveDisabledMaps()

		// Sleep for the --identity-restore-grace-period (default: 30 seconds k8s, 10 minutes kvstore), allowing
		// the normal allocation processes to finish, before releasing restored resources.
		time.Sleep(option.Config.IdentityRestoreGracePeriod)
		d.identityRestorer.ReleaseRestoredIdentities()
	}()

	// Migrating the ENI datapath must happen before the API is served to
	// prevent endpoints from being created. It also must be before the health
	// initialization logic which creates the health endpoint, for the same
	// reasons as the API being served. We want to ensure that this migration
	// logic runs before any endpoint creates.
	if option.Config.IPAM == ipamOption.IPAMENI {
		migrated, failed := linuxrouting.NewMigrator(
			d.logger,
			&eni.InterfaceDB{Clientset: params.Clientset},
		).MigrateENIDatapath(option.Config.EgressMultiHomeIPRuleCompat)
		switch {
		case failed == -1:
			// No need to handle this case specifically because it is handled
			// in the call already.
		case migrated >= 0 && failed > 0:
			d.logger.Error(fmt.Sprintf(
				"Failed to migrate ENI datapath. "+
					"%d endpoints were successfully migrated and %d failed to migrate completely. "+
					"The original datapath is still in-place, however it is recommended to retry the migration.",
				migrated, failed),
			)

		case migrated >= 0 && failed == 0:
			d.logger.Info(fmt.Sprintf(
				"Migration of ENI datapath successful, %d endpoints were migrated and none failed.",
				migrated),
			)
		}
	}

	bootstrapStats.healthCheck.Start()
	if option.Config.EnableHealthChecking {
		if err := d.ciliumHealth.Init(d.ctx, d.healthEndpointRouting, cleaner.cleanupFuncs.Add); err != nil {
			return fmt.Errorf("failed to initialize cilium health: %w", err)
		}
	}
	bootstrapStats.healthCheck.End(true)

	if err := d.monitorAgent.SendEvent(monitorAPI.MessageTypeAgent, monitorAPI.StartMessage(time.Now())); err != nil {
		d.logger.Warn("Failed to send agent start monitor message", logfields.Error, err)
	}

	d.logger.Info(
		"Daemon initialization completed",
		logfields.BootstrapTime, time.Since(bootstrapTimestamp),
	)

	bootstrapStats.overall.End(true)
	bootstrapStats.updateMetrics()

	// Start controller to validate daemon config is unchanged
	cfgGroup := controller.NewGroup("daemon-validate-config")
	d.controllers.UpdateController(
		cfgGroup.Name,
		controller.ControllerParams{
			Group:  cfgGroup,
			Health: params.Health,
			DoFunc: func(context.Context) error {
				// Validate that Daemon config has not changed, ignoring 'Opts'
				// that may be modified via config patch events.
				return option.Config.ValidateUnchanged()
			},
			// avoid synhronized run with other
			// controllers started at same time
			RunInterval: 61 * time.Second,
			Context:     d.ctx,
		})

	return nil
}

func registerEndpointStateResolver(lc cell.Lifecycle, daemonPromise promise.Promise[*Daemon], resolver promise.Resolver[endpointstate.Restorer]) {
	var wg sync.WaitGroup

	lc.Append(cell.Hook{
		OnStart: func(ctx cell.HookContext) error {
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon, err := daemonPromise.Await(context.Background())
				if err != nil {
					resolver.Reject(err)
				} else {
					resolver.Resolve(daemon)
				}
			}()
			return nil
		},
		OnStop: func(ctx cell.HookContext) error {
			wg.Wait()
			return nil
		},
	})
}

func initClockSourceOption(logger *slog.Logger) {
	option.Config.ClockSource = option.ClockSourceKtime
	option.Config.KernelHz = 1 // Known invalid non-zero to avoid div by zero.
	hz, err := probes.KernelHZ()
	if err != nil {
		logger.Info(
			fmt.Sprintf("Auto-disabling %q feature since KERNEL_HZ cannot be determined", option.EnableBPFClockProbe),
			logfields.Error, err,
		)
		option.Config.EnableBPFClockProbe = false
	} else {
		option.Config.KernelHz = int(hz)
	}

	if option.Config.EnableBPFClockProbe {
		t, err := probes.Jiffies()
		if err == nil && t > 0 {
			option.Config.ClockSource = option.ClockSourceJiffies
		} else {
			logger.Warn(
				fmt.Sprintf("Auto-disabling %q feature since kernel doesn't expose jiffies", option.EnableBPFClockProbe),
				logfields.Error, err,
			)
			option.Config.EnableBPFClockProbe = false
		}
	}
}
