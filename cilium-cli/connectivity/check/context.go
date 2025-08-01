// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package check

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blang/semver/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cilium/cilium/api/v1/observer"
	"github.com/cilium/cilium/cilium-cli/connectivity/perf/common"
	"github.com/cilium/cilium/cilium-cli/defaults"
	"github.com/cilium/cilium/cilium-cli/k8s"
	"github.com/cilium/cilium/cilium-cli/sysdump"
	"github.com/cilium/cilium/cilium-cli/utils/features"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	slimcorev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/tools/testowners/codeowners"
)

const (
	socatMulticastTestMsg = "Multicast test message"
)

// ConnectivityTest is the root context of the connectivity test suite
// and holds all resources belonging to it. It implements interface
// ConnectivityTest and is instantiated once at the start of the program,
type ConnectivityTest struct {
	// Client connected to a Kubernetes cluster.
	client       *k8s.Client
	hubbleClient observer.ObserverClient

	// CiliumVersion is the detected or assumed version of the Cilium agent
	CiliumVersion semver.Version

	// Features contains the features enabled on the running Cilium cluster
	Features features.Set

	CodeOwners *codeowners.Ruleset

	// ClusterNameLocal is the identifier of the local cluster.
	ClusterNameLocal string
	// ClusterNameRemote is the identifier of the destination cluster.
	ClusterNameRemote string

	// Parameters to the test suite, specified by the CLI user.
	params Parameters

	sysdumpHooks sysdump.Hooks

	logger *ConcurrentLogger

	// Clients for source and destination clusters.
	clients *deploymentClients

	ciliumPods           map[string]Pod
	echoPods             map[string]Pod
	echoExternalPods     map[string]Pod
	clientPods           map[string]Pod
	clientCPPods         map[string]Pod
	l7LBClientPods       map[string]Pod
	perfClientPods       []Pod
	perfServerPod        []Pod
	perfProfilingPods    map[string]Pod
	PerfResults          []common.PerfSummary
	echoServices         map[string]Service
	echoExternalServices map[string]Service
	ingressService       map[string]Service
	l7LBService          map[string]Service
	k8sService           Service
	lrpClientPods        map[string]Pod
	lrpBackendPods       map[string]Pod
	frrPods              []Pod
	socatServerPods      []Pod
	socatClientPods      []Pod

	hostNetNSPodsByNode      map[string]Pod
	secondaryNetworkNodeIPv4 map[string]string // node name => secondary ip
	secondaryNetworkNodeIPv6 map[string]string // node name => secondary ip

	tests     []*Test
	testNames map[string]struct{}

	lastFlowTimestamps map[string]time.Time

	nodes              map[string]*slimcorev1.Node
	controlPlaneNodes  map[string]*slimcorev1.Node
	nodesWithoutCilium map[string]struct{}
	ciliumNodes        map[NodeIdentity]*ciliumv2.CiliumNode

	testConnDisruptClientNSTrafficDeploymentNames []string
}

// NodeIdentity uniquely identifies a Node by Cluster and Name.
type NodeIdentity struct{ Cluster, Name string }

func netIPToCIDRs(netIPs []netip.Addr) (netCIDRs []netip.Prefix) {
	for _, ip := range netIPs {
		found := false
		for _, cidr := range netCIDRs {
			if cidr.Addr().Is4() == ip.Is4() && cidr.Contains(ip) {
				found = true
				break
			}
		}
		if found {
			continue
		}

		// Generate a /24 or /64 accordingly
		bits := 24
		if ip.Is6() {
			bits = 64
		}
		netCIDRs = append(netCIDRs, netip.PrefixFrom(ip, bits).Masked())
	}
	return
}

// debug returns the value of the user-provided debug flag.
func (ct *ConnectivityTest) debug() bool {
	return ct.params.Debug
}

// timestamp returns the value of the user-provided timestamp flag.
func (ct *ConnectivityTest) timestamp() bool {
	return ct.params.Timestamp
}

// actions returns a list of all Actions registered under the test context.
func (ct *ConnectivityTest) actions() []*Action {
	var out []*Action

	for _, t := range ct.tests {
		for _, al := range t.scenarios {
			out = append(out, al...)
		}
	}

	return out
}

// skippedTests returns a list of Tests that were marked as skipped at the
// start of the test suite.
func (ct *ConnectivityTest) skippedTests() []*Test {
	var out []*Test

	for _, t := range ct.tests {
		if t.skipped {
			out = append(out, t)
		}
	}

	return out
}

// skippedScenarios returns a list of Scenarios that were marked as skipped.
func (ct *ConnectivityTest) skippedScenarios() []Scenario {
	var out []Scenario

	for _, t := range ct.tests {
		out = append(out, t.scenariosSkipped...)
	}

	return out
}

// failedTests returns a list of Tests that encountered a failure.
func (ct *ConnectivityTest) failedTests() []*Test {
	var out []*Test

	for _, t := range ct.tests {
		if t.skipped {
			continue
		}
		if t.failed {
			out = append(out, t)
		}
	}

	return out
}

// failedActions returns a list of all failed Actions.
func (ct *ConnectivityTest) failedActions() []*Action {
	var out []*Action

	for _, t := range ct.failedTests() {
		out = append(out, t.failedActions()...)
	}

	return out
}

// NewConnectivityTest returns a new ConnectivityTest.
func NewConnectivityTest(
	client *k8s.Client,
	p Parameters,
	sysdumpHooks sysdump.Hooks,
	logger *ConcurrentLogger,
	owners *codeowners.Ruleset,
) (*ConnectivityTest, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}

	k := &ConnectivityTest{
		client:                   client,
		params:                   p,
		sysdumpHooks:             sysdumpHooks,
		logger:                   logger,
		ciliumPods:               make(map[string]Pod),
		echoPods:                 make(map[string]Pod),
		echoExternalPods:         make(map[string]Pod),
		clientPods:               make(map[string]Pod),
		clientCPPods:             make(map[string]Pod),
		l7LBClientPods:           make(map[string]Pod),
		lrpClientPods:            make(map[string]Pod),
		lrpBackendPods:           make(map[string]Pod),
		perfProfilingPods:        make(map[string]Pod),
		socatServerPods:          []Pod{},
		socatClientPods:          []Pod{},
		perfClientPods:           []Pod{},
		perfServerPod:            []Pod{},
		PerfResults:              []common.PerfSummary{},
		echoServices:             make(map[string]Service),
		echoExternalServices:     make(map[string]Service),
		ingressService:           make(map[string]Service),
		l7LBService:              make(map[string]Service),
		hostNetNSPodsByNode:      make(map[string]Pod),
		secondaryNetworkNodeIPv4: make(map[string]string),
		secondaryNetworkNodeIPv6: make(map[string]string),
		nodes:                    make(map[string]*slimcorev1.Node),
		nodesWithoutCilium:       make(map[string]struct{}),
		ciliumNodes:              make(map[NodeIdentity]*ciliumv2.CiliumNode),
		tests:                    []*Test{},
		testNames:                make(map[string]struct{}),
		lastFlowTimestamps:       make(map[string]time.Time),
		Features:                 features.Set{},
		CodeOwners:               owners,
	}

	return k, nil
}

// AddTest adds a new test scope within the ConnectivityTest and returns a new Test.
func (ct *ConnectivityTest) AddTest(t *Test) *Test {
	if _, ok := ct.testNames[t.name]; ok {
		ct.Fatalf("test %s exists in suite", t.name)
	}
	t.ctx = ct
	ct.tests = append(ct.tests, t)
	ct.testNames[t.name] = struct{}{}
	return t
}

// GetTest returns the test scope for test named "name" if found,
// a non-nil error otherwise.
func (ct *ConnectivityTest) GetTest(name string) (*Test, error) {
	if _, ok := ct.testNames[name]; !ok {
		return nil, fmt.Errorf("test %s not found", name)
	}

	for _, t := range ct.tests {
		if t.name == name {
			return t, nil
		}
	}

	panic("missing test descriptor for a registered name")
}

// MustGetTest returns the test scope for test named "name" if found,
// or panics otherwise.
func (ct *ConnectivityTest) MustGetTest(name string) *Test {
	test, err := ct.GetTest(name)
	if err != nil {
		panic(err)
	}
	return test
}

// SetupHooks defines the extension hooks executed during the setup of the connectivity tests.
type SetupHooks interface {
	// DetectFeatures is an hook to perform the detection of extra features.
	DetectFeatures(ctx context.Context, ct *ConnectivityTest) error
	// SetupAndValidate is an hook to setup additional connectivity test dependencies.
	SetupAndValidate(ctx context.Context, ct *ConnectivityTest) error
}

// SetupAndValidate sets up and validates the connectivity test infrastructure
// such as the client pods and validates the deployment of them along with
// Cilium. This must be run before Run() is called.
func (ct *ConnectivityTest) SetupAndValidate(ctx context.Context, extra SetupHooks) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	setupAndValidate := ct.setupAndValidate
	if ct.Params().Perf {
		setupAndValidate = ct.setupAndValidatePerf
	}

	if err := setupAndValidate(ctx, extra); err != nil {
		return err
	}

	// Setup and validate all the extras coming from extended functionalities.
	return extra.SetupAndValidate(ctx, ct)
}

func (ct *ConnectivityTest) setupAndValidate(ctx context.Context, extra SetupHooks) error {
	if err := ct.detectSingleNode(ctx); err != nil {
		return err
	}
	if err := ct.initClients(ctx); err != nil {
		return err
	}
	if err := ct.initCiliumPods(ctx); err != nil {
		return err
	}
	if err := ct.getNodes(ctx); err != nil {
		return err
	}
	// Detect Cilium version after Cilium pods have been initialized and before feature
	// detection.
	if err := ct.detectCiliumVersion(ctx); err != nil {
		return err
	}
	if err := ct.detectFeatures(ctx); err != nil {
		return err
	}
	if err := extra.DetectFeatures(ctx, ct); err != nil {
		return err
	}

	if ct.debug() {
		ct.Debug("Detected features:")
		for _, f := range slices.Sorted(maps.Keys(ct.Features)) {
			ct.Debugf("  %s: %s", f, ct.Features[f])
		}
	}

	if ct.FlowAggregation() {
		ct.Info("Monitor aggregation detected, will skip some flow validation steps")
	}

	if err := ct.deploy(ctx); err != nil {
		return err
	}
	if err := ct.validateDeployment(ctx); err != nil {
		return err
	}
	if err := ct.patchDeployment(ctx); err != nil {
		return err
	}
	if err := ct.getCiliumNodes(ctx); err != nil {
		return err
	}
	if ct.params.Hubble {
		if err := ct.enableHubbleClient(ctx); err != nil {
			return fmt.Errorf("unable to create hubble client: %w", err)
		}
	}
	if match, _ := ct.Features.MatchRequirements(features.RequireEnabled(features.NodeWithoutCilium)); match {
		ct.detectPodCIDRs()

		if err := ct.detectNodesWithoutCiliumIPs(); err != nil {
			return fmt.Errorf("unable to detect nodes w/o Cilium IPs: %w", err)
		}
	}
	if match, _ := ct.Features.MatchRequirements(features.RequireEnabled(features.CIDRMatchNodes)); match {
		if err := ct.detectNodeCIDRs(ctx); err != nil {
			return fmt.Errorf("unable to detect node CIDRs: %w", err)
		}
	}
	if ct.params.K8sLocalHostTest {
		if err := ct.detectK8sCIDR(ctx); err != nil {
			return fmt.Errorf("unable to detect K8s CIDR: %w", err)
		}
	}

	return nil
}

func (ct *ConnectivityTest) setupAndValidatePerf(ctx context.Context, _ SetupHooks) error {
	if err := ct.initClients(ctx); err != nil {
		return err
	}

	if err := ct.deployPerf(ctx); err != nil {
		return err
	}

	if err := ct.validateDeploymentPerf(ctx); err != nil {
		return err
	}

	return nil
}

// PrintTestInfo prints connectivity test names and count.
func (ct *ConnectivityTest) PrintTestInfo() {
	if len(ct.tests) == 0 {
		return
	}
	ct.Debugf("[%s] Registered connectivity tests", ct.params.TestNamespace)
	for _, t := range ct.tests {
		ct.Debugf("  %s", t)
	}
	// Newline denoting start of test output.
	ct.Logf("🏃[%s] Running %d tests ...", ct.params.TestNamespace, len(ct.tests))
}

// Run kicks off execution of all Tests registered to the ConnectivityTest.
// Each Test's Run() method is called within its own goroutine.
func (ct *ConnectivityTest) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(ct.tests) == 0 {
		return nil
	}
	// Execute all tests in the order they were registered by the test suite.
	for i, t := range ct.tests {
		if err := ctx.Err(); err != nil {
			return err
		}

		done := make(chan bool)

		go func() {
			defer func() {
				ct.logger.FinishTest(t)
				done <- true
			}()

			if err := t.Run(ctx, i+1); err != nil {
				// We know for sure we're inside a separate goroutine, so Fatal()
				// is safe and will properly record failure statistics.
				t.Fatalf("[%s] test %s failed: %s", ct.params.TestNamespace, t.Name(), err)
			}

			// Exit immediately if context was cancelled.
			if err := ctx.Err(); err != nil {
				return
			}

			// Pause after each test run if requested by the user.
			if duration := ct.PostTestSleepDuration(); duration != time.Duration(0) {
				ct.Infof("[%s] Pausing for %s after test %s", ct.params.TestNamespace, duration, t)
				time.Sleep(duration)
			}
		}()

		// Waiting for the goroutine to finish before starting another Test.
		<-done
	}
	return nil
}

// PrintReport print connectivity test instance run report.
func (ct *ConnectivityTest) PrintReport(ctx context.Context) error {
	if len(ct.tests) == 0 {
		return nil
	}

	if ct.Params().FlushCT {
		var wg sync.WaitGroup

		wg.Add(len(ct.CiliumPods()))
		for _, ciliumPod := range ct.CiliumPods() {
			cmd := strings.Split("cilium bpf ct flush global", " ")
			go func(ctx context.Context, pod Pod) {
				defer wg.Done()

				ct.Debugf("Flushing CT entries in %s/%s", pod.Pod.Namespace, pod.Pod.Name)
				_, err := pod.K8sClient.ExecInPod(ctx, pod.Pod.Namespace, pod.Pod.Name, defaults.AgentContainerName, cmd)
				if err != nil {
					ct.Fatalf("failed to flush ct entries: %v", err)
				}
			}(ctx, ciliumPod)
		}

		wg.Wait()
	}
	// Report the test results.
	return ct.report()
}

// Cleanup cleans test related fields.
// So, ConnectivityTest instance can be re-used.
func (ct *ConnectivityTest) Cleanup() {
	ct.testNames = make(map[string]struct{})
	ct.tests = make([]*Test, 0)
	ct.lastFlowTimestamps = make(map[string]time.Time)
}

// skip marks the Test as skipped.
func (ct *ConnectivityTest) skip(t *Test, index int, reason string) {
	ct.logger.Printf(t, "[=] [%s] Skipping test [%s] [%d/%d] (%s)\n", ct.params.TestNamespace, t.Name(), index, len(t.ctx.tests), reason)
	t.skipped = true
}

func (ct *ConnectivityTest) report() error {
	total := ct.tests
	actions := ct.actions()
	skippedTests := ct.skippedTests()
	skippedScenarios := ct.skippedScenarios()
	failed := ct.failedTests()

	nt := len(total)
	na := len(actions)
	nst := len(skippedTests)
	nss := len(skippedScenarios)
	nf := len(failed)

	if nf > 0 {
		ct.Header(fmt.Sprintf("📋 Test Report [%s]", ct.params.TestNamespace))

		// There are failed tests, fetch all failed actions.
		fa := len(ct.failedActions())

		ct.Failf("%d/%d tests failed (%d/%d actions), %d tests skipped, %d scenarios skipped:", nf, nt-nst, fa, na, nst, nss)

		// List all failed actions by test.
		failedActions := 0
		for _, t := range failed {
			ct.Logf("Test [%s]:", t.Name())
			for _, a := range t.failedActions() {
				failedActions++
				if a.failureMessage != "" {
					ct.Logf("  🟥 %s: %s", a, a.failureMessage)
				} else {
					ct.Log("  ❌", a)
				}
				ct.LogOwners(a.Scenario())
			}
		}
		if len(failed) > 0 && failedActions == 0 {
			allScenarios := make([]codeowners.Scenario, 0, len(failed))
			for _, t := range failed {
				for scenario := range t.scenarios {
					allScenarios = append(allScenarios, scenario)
				}
			}
			if len(allScenarios) == 0 {
				// Test failure was triggered not by a specific action
				// failing, but some other infrastructure code.
				allScenarios = []codeowners.Scenario{defaultTestOwners}
			}
			ct.LogOwners(allScenarios...)
		}

		return fmt.Errorf("[%s] %d tests failed", ct.params.TestNamespace, nf)
	}

	if ct.params.Perf && !ct.params.PerfParameters.NetQos && !ct.params.PerfParameters.Bandwidth {
		ct.Header(fmt.Sprintf("🔥 Network Performance Test Summary [%s]:", ct.params.TestNamespace))
		ct.Logf("%s", strings.Repeat("-", 200))
		ct.Logf("📋 %-15s | %-10s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s", "Scenario", "Node", "Test", "Duration", "Min", "Mean", "Max", "P50", "P90", "P99", "Transaction rate OP/s")
		ct.Logf("%s", strings.Repeat("-", 200))
		nodeString := func(sameNode bool) string {
			if sameNode {
				return "same-node"
			}
			return "other-node"
		}
		for _, result := range ct.PerfResults {
			if result.Result.Latency != nil && result.Result.TransactionRateMetric != nil {
				ct.Logf("📋 %-15s | %-10s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-15s | %-12.2f",
					result.PerfTest.Scenario,
					nodeString(result.PerfTest.SameNode),
					result.PerfTest.Test,
					result.PerfTest.Duration,
					result.Result.Latency.Min,
					result.Result.Latency.Avg,
					result.Result.Latency.Max,
					result.Result.Latency.Perc50,
					result.Result.Latency.Perc90,
					result.Result.Latency.Perc99,
					result.Result.TransactionRateMetric.TransactionRate,
				)
			}
		}
		ct.Logf("%s", strings.Repeat("-", 200))
		ct.Logf("%s", strings.Repeat("-", 88))
		ct.Logf("📋 %-15s | %-10s | %-18s | %-15s | %-15s ", "Scenario", "Node", "Test", "Duration", "Throughput Mb/s")
		ct.Logf("%s", strings.Repeat("-", 88))
		for _, result := range ct.PerfResults {
			if result.Result.ThroughputMetric != nil {
				ct.Logf("📋 %-15s | %-10s | %-18s | %-15s | %-12.2f ",
					result.PerfTest.Scenario,
					nodeString(result.PerfTest.SameNode),
					result.PerfTest.Test,
					result.PerfTest.Duration,
					result.Result.ThroughputMetric.Throughput/1000000,
				)
			}
		}
		ct.Logf("%s", strings.Repeat("-", 88))
		if ct.Params().PerfParameters.ReportDir != "" {
			common.ExportPerfSummaries(ct.PerfResults, ct.Params().PerfParameters.ReportDir)
		}
	}

	ct.Headerf("✅ [%s] All %d tests (%d actions) successful, %d tests skipped, %d scenarios skipped.", ct.params.TestNamespace, nt-nst, na, nst, nss)

	return nil
}

func (ct *ConnectivityTest) enableHubbleClient(ctx context.Context) error {
	ct.Log("🔭 Enabling Hubble telescope...")

	c, err := grpc.NewClient(ct.params.HubbleServer, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	ct.hubbleClient = observer.NewObserverClient(c)

	status, err := ct.hubbleClient.ServerStatus(ctx, &observer.ServerStatusRequest{})
	if err != nil {
		ct.Warn("Unable to contact Hubble Relay, disabling Hubble telescope and flow validation:", err)
		ct.Info(`Expose Relay locally with:
   cilium hubble enable
   cilium hubble port-forward&`)
		ct.hubbleClient = nil
		ct.params.Hubble = false

		if ct.params.FlowValidation == FlowValidationModeStrict {
			ct.Fail("In --flow-validation=strict mode, Hubble must be available to validate flows")
			return fmt.Errorf("hubble is not available: %w", err)
		}
		return nil
	}

	for status.GetNumUnavailableNodes().GetValue() > 0 {
		ct.Infof("Waiting for %d nodes to become available: %s",
			status.GetNumUnavailableNodes().GetValue(), status.GetUnavailableNodes())
		time.Sleep(5 * time.Second)
		status, err = ct.hubbleClient.ServerStatus(ctx, &observer.ServerStatusRequest{})
		if err != nil {
			ct.Failf("Not all nodes became available to Hubble Relay: %v", err)
			return fmt.Errorf("not all nodes became available to Hubble Relay: %w", err)
		}
	}
	ct.Infof("Hubble is OK, flows: %d/%d, connected nodes: %d, unavailable nodes %d",
		status.NumFlows, status.MaxFlows, status.GetNumConnectedNodes().GetValue(), status.GetNumUnavailableNodes().GetValue())
	return nil
}

func (ct *ConnectivityTest) detectPodCIDRs() {
	for id, n := range ct.CiliumNodes() {
		if _, ok := ct.nodesWithoutCilium[id.Name]; ok {
			// Skip the nodes where Cilium is not installed.
			continue
		}

		pod, ok := ct.hostNetNSPodsByNode[id.Name]
		if !ok {
			// No host-netns pod seems to be running on this node. Skipping
			ct.Warnf("Could not find any host-netns pod running on %s", id.Name)
			continue
		}

		// PodIPs match HostIPs given that the pod is running in host network.
		hostIPs := pod.Pod.Status.PodIPs

		for _, cidr := range n.Spec.IPAM.PodCIDRs {
			ct.params.PodCIDRs = append(ct.params.PodCIDRs, toPodCIDRs(cidr, hostIPs...)...)
		}

		// additional IP pools from multi-pool IPAM mode
		for _, pool := range n.Spec.IPAM.Pools.Allocated {
			for _, podCIDR := range pool.CIDRs {
				ct.params.PodCIDRs = append(ct.params.PodCIDRs, toPodCIDRs(string(podCIDR), hostIPs...)...)
			}
		}
	}
}

func toPodCIDRs(cidr string, podIPs ...corev1.PodIP) []podCIDRs {
	var podCIDRsInfo []podCIDRs
	for _, ip := range podIPs {
		f := features.GetIPFamily(ip.IP)
		if strings.Contains(cidr, ":") != (f == features.IPFamilyV6) {
			// Skip if the host IP of the pod mismatches with pod CIDR.
			// Cannot create a route with the gateway IP family
			// mismatching the subnet.
			continue
		}
		podCIDRsInfo = append(podCIDRsInfo, podCIDRs{cidr, ip.IP})
	}
	return podCIDRsInfo
}

// detectNodeCIDRs produces one or more CIDRs that cover all nodes in the cluster.
// ipv4 addresses are collapsed in to one or more /24s, and v6 to one or more /64s
func (ct *ConnectivityTest) detectNodeCIDRs(ctx context.Context) error {
	if len(ct.params.NodeCIDRs) > 0 {
		return nil
	}

	nodes, err := ct.client.ListSlimNodes(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	nodeIPs := make([]netip.Addr, 0, len(nodes.Items))
	cPIPs := make([]netip.Addr, 0, 1)

	for i, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type != slimcorev1.NodeInternalIP {
				continue
			}

			ip, err := netip.ParseAddr(addr.Address)
			if err != nil {
				continue
			}
			nodeIPs = append(nodeIPs, ip)
			if isControlPlane(&nodes.Items[i]) {
				cPIPs = append(cPIPs, ip)
			}
		}
	}

	if len(nodeIPs) == 0 {
		return fmt.Errorf("detectNodeCIDRs failed: no node IPs disovered")
	}

	// collapse set of IPs in to CIDRs
	nodeCIDRs := netIPToCIDRs(nodeIPs)
	cPCIDRs := netIPToCIDRs(cPIPs)

	ct.params.NodeCIDRs = make([]string, 0, len(nodeCIDRs))
	for _, cidr := range nodeCIDRs {
		ct.params.NodeCIDRs = append(ct.params.NodeCIDRs, cidr.String())
	}
	ct.params.ControlPlaneCIDRs = make([]string, 0, len(cPCIDRs))
	for _, cidr := range cPCIDRs {
		ct.params.ControlPlaneCIDRs = append(ct.params.ControlPlaneCIDRs, cidr.String())
	}
	ct.Debugf("Detected NodeCIDRs: %v", ct.params.NodeCIDRs)
	return nil
}

// detectK8sCIDR produces one CIDR that covers the kube-apiserver address.
// ipv4 addresses are collapsed in to one or more /24s, and v6 to one or more /64s
func (ct *ConnectivityTest) detectK8sCIDR(ctx context.Context) error {
	service, err := ct.client.GetService(ctx, "default", "kubernetes", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get \"kubernetes.default\" service: %w", err)
	}
	addr, err := netip.ParseAddr(service.Spec.ClusterIP)
	if err != nil {
		return fmt.Errorf("failed to parse \"kubernetes.default\" service Cluster IP: %w", err)
	}

	// Generate a /24 or /64 accordingly
	bits := 24
	if addr.Is6() {
		bits = 64
	}
	ct.params.K8sCIDR = netip.PrefixFrom(addr, bits).Masked().String()
	ct.k8sService = Service{Service: service, URLPath: "/healthz"}
	ct.Debugf("Detected K8sCIDR: %q", ct.params.K8sCIDR)
	return nil
}

func (ct *ConnectivityTest) detectNodesWithoutCiliumIPs() error {
	for n := range ct.nodesWithoutCilium {
		pod := ct.hostNetNSPodsByNode[n]
		for _, ip := range pod.Pod.Status.PodIPs {
			hostIP, err := netip.ParseAddr(ip.IP)
			if err != nil {
				return fmt.Errorf("unable to parse nodes without Cilium IP addr %q: %w", ip.IP, err)
			}
			ct.params.NodesWithoutCiliumIPs = append(ct.params.NodesWithoutCiliumIPs,
				nodesWithoutCiliumIP{ip.IP, hostIP.BitLen()})
		}
	}

	return nil
}

func (ct *ConnectivityTest) modifyStaticRoutesForNodesWithoutCilium(ctx context.Context, verb string) error {
	for _, e := range ct.params.PodCIDRs {
		for withoutCilium := range ct.nodesWithoutCilium {
			pod := ct.hostNetNSPodsByNode[withoutCilium]
			_, err := ct.client.ExecInPod(ctx, pod.Pod.Namespace, pod.Pod.Name, hostNetNSDeploymentNameNonCilium,
				[]string{"ip", "route", verb, e.CIDR, "via", e.HostIP},
			)
			ct.Debugf("Modifying (%s) static route on nodes without Cilium (%v): %v",
				verb, withoutCilium,
				[]string{"ip", "route", verb, e.CIDR, "via", e.HostIP},
			)
			if err != nil {
				return fmt.Errorf("failed to %s static route: %w", verb, err)
			}
		}
	}

	return nil
}

// multiClusterClientLock protects K8S client instantiation (Scheme registration)
// for the cluster mesh setup in case of connectivity test concurrency > 1
var multiClusterClientLock = lock.Mutex{}

// determine if only single node tests can be ran.
// if the user specified SingleNode on the CLI this is taken as the truth and
// we simply return.
//
// otherwise, list nodes and check for NoSchedule taints, as long as we have > 1
// schedulable nodes we will run multi-node tests.
func (ct *ConnectivityTest) detectSingleNode(ctx context.Context) error {
	if ct.params.MultiCluster != "" && ct.params.SingleNode {
		return fmt.Errorf("single-node test can not be enabled with multi-cluster test")
	}

	// single node explicitly defined by user.
	if ct.params.SingleNode {
		return nil
	}

	// only detect single node for single cluster environments
	if ct.params.MultiCluster != "" {
		return nil
	}

	daemonSet, err := ct.client.GetDaemonSet(ctx, ct.params.CiliumNamespace, ct.params.AgentDaemonSetName, metav1.GetOptions{})
	if err != nil {
		ct.Fatal("Unable to determine status of Cilium DaemonSet. Run \"cilium status\" for more details")
		return fmt.Errorf("unable to determine status of Cilium DaemonSet: %w", err)
	}

	if daemonSet.Status.DesiredNumberScheduled == 1 {
		ct.params.SingleNode = true
		return nil
	}

	nodes, err := ct.client.ListSlimNodes(ctx, metav1.ListOptions{})
	if err != nil {
		ct.Fatal("Unable to list nodes.")
		return fmt.Errorf("unable to list nodes: %w", err)
	}

	numWorkerNodes := len(nodes.Items)
	for _, n := range nodes.Items {
		for _, t := range n.Spec.Taints {
			switch {
			case (t.Key == "node-role.kubernetes.io/master" && t.Effect == "NoSchedule"):
				numWorkerNodes--
			case (t.Key == "node-role.kubernetes.io/control-plane" && t.Effect == "NoSchedule"):
				numWorkerNodes--
			}
		}
	}

	ct.params.SingleNode = numWorkerNodes == 1
	if ct.params.SingleNode {
		ct.Info("Single-node environment detected, enabling single-node connectivity test")
	}

	return nil
}

// initClients assigns the k8s clients used for connectivity tests.
// in the event that this is a multi-cluster test scenario the destination k8s
// client is set to the cluster provided in the MultiCluster parameter.
func (ct *ConnectivityTest) initClients(ctx context.Context) error {
	c := &deploymentClients{
		src: ct.client,
		dst: ct.client,
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if ct.params.MultiCluster != "" {
		multiClusterClientLock.Lock()
		defer multiClusterClientLock.Unlock()
		dst, err := k8s.NewClient(ct.params.MultiCluster, "", ct.params.CiliumNamespace, ct.params.ImpersonateAs, ct.params.ImpersonateGroups)
		if err != nil {
			return fmt.Errorf("unable to create Kubernetes client for remote cluster %q: %w", ct.params.MultiCluster, err)
		}

		c.dst = dst
	}

	ct.clients = c

	return nil
}

// initCiliumPods fetches the Cilium agent pod information from all clients
func (ct *ConnectivityTest) initCiliumPods(ctx context.Context) error {
	for _, client := range ct.clients.clients() {
		ciliumPods, err := client.ListPods(ctx, ct.params.CiliumNamespace, metav1.ListOptions{LabelSelector: ct.params.AgentPodSelector})
		if err != nil {
			return fmt.Errorf("unable to list Cilium pods: %w", err)
		}
		if len(ciliumPods.Items) == 0 {
			return fmt.Errorf("no cilium agent pods found in -n %s -l %s", ct.params.CiliumNamespace, ct.params.AgentPodSelector)
		}
		for _, ciliumPod := range ciliumPods.Items {
			// TODO: Can Cilium pod names collide across clusters?
			ct.ciliumPods[ciliumPod.Name] = Pod{
				K8sClient: client,
				Pod:       ciliumPod.DeepCopy(),
			}
		}
	}

	return nil
}

func (ct *ConnectivityTest) getNodes(ctx context.Context) error {
	ct.nodes = make(map[string]*slimcorev1.Node)
	ct.controlPlaneNodes = make(map[string]*slimcorev1.Node)
	ct.nodesWithoutCilium = make(map[string]struct{})
	nodeList, err := ct.client.ListSlimNodes(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list K8s Nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		if canNodeRunCilium(&node) {
			if isControlPlane(&node) {
				ct.controlPlaneNodes[node.ObjectMeta.Name] = node.DeepCopy()
			}
			ct.nodes[node.ObjectMeta.Name] = node.DeepCopy()
		} else {
			ct.nodesWithoutCilium[node.ObjectMeta.Name] = struct{}{}
		}
	}

	return nil
}

func (ct *ConnectivityTest) getCiliumNodes(ctx context.Context) error {
	for _, client := range ct.Clients() {
		nodeList, err := client.ListCiliumNodes(ctx)
		if err != nil {
			return fmt.Errorf("unable to list CiliumNodes: %w", err)
		}

		for _, node := range nodeList.Items {
			ct.ciliumNodes[NodeIdentity{client.ClusterName(), node.ObjectMeta.Name}] = node.DeepCopy()
		}
	}

	return nil
}

// DetectMinimumCiliumVersion returns the smallest Cilium version running in
// the cluster(s)
func (ct *ConnectivityTest) DetectMinimumCiliumVersion(ctx context.Context) (*semver.Version, error) {
	var minVersion *semver.Version
	for name, ciliumPod := range ct.ciliumPods {
		podVersion, err := ciliumPod.K8sClient.GetCiliumVersion(ctx, ciliumPod.Pod)
		if err != nil {
			return nil, fmt.Errorf("unable to parse Cilium version on pod %q: %w", name, err)
		}
		if minVersion == nil || podVersion.LT(*minVersion) {
			minVersion = podVersion
		}
	}

	if minVersion == nil {
		return nil, errors.New("unable to detect minimum Cilium version")
	}
	return minVersion, nil
}

func (ct *ConnectivityTest) CurlCommandWithOutput(peer TestPeer, ipFam features.IPFamily, expectingSuccess bool, opts []string) []string {
	cmd := []string{
		"curl", "--silent", "--fail", "--show-error",
	}

	if connectTimeout := ct.params.ConnectTimeout.Seconds(); connectTimeout > 0.0 {
		cmd = append(cmd, "--connect-timeout", strconv.FormatFloat(connectTimeout, 'f', -1, 64))
	}
	if requestTimeout := ct.params.RequestTimeout.Seconds(); requestTimeout > 0.0 {
		cmd = append(cmd, "--max-time", strconv.FormatFloat(requestTimeout, 'f', -1, 64))
	}
	if ct.params.CurlInsecure {
		cmd = append(cmd, "--insecure")
	}

	switch ipFam {
	case features.IPFamilyV4:
		cmd = append(cmd, "-4")
	case features.IPFamilyV6:
		cmd = append(cmd, "-6")
	}

	if host := peer.Address(ipFam); strings.HasSuffix(host, ".") {
		// Let's explicitly configure the Host header in case the DNS name has a
		// trailing dot. This allows us to use trailing dots to prevent system
		// resolvers from appending suffixes from the search list, while
		// circumventing shenanigans associated with the host header including
		// the trailing dot.
		cmd = append(cmd, "-H", fmt.Sprintf("Host: %s", strings.TrimSuffix(host, ".")))
	}

	numTargets := 1
	if expectingSuccess && ct.params.CurlParallel > 0 {
		numTargets = int(ct.params.CurlParallel)
		cmd = append(cmd, "--parallel", "--parallel-immediate")
	}

	cmd = append(cmd, opts...)
	url := fmt.Sprintf("%s://%s%s",
		peer.Scheme(),
		net.JoinHostPort(peer.Address(ipFam), fmt.Sprint(peer.Port())),
		peer.Path())

	for range numTargets {
		cmd = append(cmd, url)
	}

	return cmd
}

func (ct *ConnectivityTest) CurlCommand(peer TestPeer, ipFam features.IPFamily, expectingSuccess bool, opts []string) []string {
	return ct.CurlCommandWithOutput(peer, ipFam, expectingSuccess, append([]string{
		"-w", "%{local_ip}:%{local_port} -> %{remote_ip}:%{remote_port} = %{response_code}\n",
		"--output", "/dev/null",
	}, opts...))
}

func (ct *ConnectivityTest) PingCommand(peer TestPeer, ipFam features.IPFamily, extraArgs ...string) []string {
	cmd := []string{"ping", "-c", "1"}

	if ipFam == features.IPFamilyV6 {
		cmd = append(cmd, "-6")
	}

	if connectTimeout := ct.params.ConnectTimeout.Seconds(); connectTimeout > 0.0 {
		cmd = append(cmd, "-W", strconv.FormatFloat(connectTimeout, 'f', -1, 64))
	}

	cmd = append(cmd, extraArgs...)

	cmd = append(cmd, peer.Address(ipFam))

	return cmd
}

func (ct *ConnectivityTest) DigCommand(peer TestPeer, ipFam features.IPFamily) []string {
	cmd := []string{"dig", "+time=2", "kubernetes"}

	cmd = append(cmd, fmt.Sprintf("@%s", peer.Address(ipFam)))
	return cmd
}

func (ct *ConnectivityTest) NSLookupCommandService(peer TestPeer, ipFam features.IPFamily) []string {
	cmd := []string{"nslookup"}
	if ipFam == features.IPFamilyV4 {
		cmd = append(cmd, "-type=A")
	} else if ipFam == features.IPFamilyV6 {
		cmd = append(cmd, "-type=AAAA")
	}
	cmd = append(cmd, "-timeout=2", peer.Address(features.IPFamilyAny))
	return cmd
}

func (ct *ConnectivityTest) RandomClientPod() *Pod {
	for _, p := range ct.clientPods {
		return &p
	}
	return nil
}

func (ct *ConnectivityTest) Params() Parameters {
	return ct.params
}

func (ct *ConnectivityTest) CiliumPods() map[string]Pod {
	return ct.ciliumPods
}

func (ct *ConnectivityTest) Nodes() map[string]*slimcorev1.Node {
	return ct.nodes
}

func (ct *ConnectivityTest) ControlPlaneNodes() map[string]*slimcorev1.Node {
	return ct.controlPlaneNodes
}

func (ct *ConnectivityTest) CiliumNodes() map[NodeIdentity]*ciliumv2.CiliumNode {
	return ct.ciliumNodes
}

func (ct *ConnectivityTest) ClientPods() map[string]Pod {
	return ct.clientPods
}

func (ct *ConnectivityTest) ControlPlaneClientPods() map[string]Pod {
	return ct.clientCPPods
}

func (ct *ConnectivityTest) L7LBClientPods() map[string]Pod {
	return ct.l7LBClientPods
}

func (ct *ConnectivityTest) HostNetNSPodsByNode() map[string]Pod {
	return ct.hostNetNSPodsByNode
}

func (ct *ConnectivityTest) SecondaryNetworkNodeIPv4() map[string]string {
	return ct.secondaryNetworkNodeIPv4
}

func (ct *ConnectivityTest) SecondaryNetworkNodeIPv6() map[string]string {
	return ct.secondaryNetworkNodeIPv6
}

func (ct *ConnectivityTest) PerfServerPod() []Pod {
	return ct.perfServerPod
}

func (ct *ConnectivityTest) PerfClientPods() []Pod {
	return ct.perfClientPods
}

func (ct *ConnectivityTest) PerfProfilingPods() map[string]Pod {
	return ct.perfProfilingPods
}

func (ct *ConnectivityTest) SocatServerPods() []Pod {
	return ct.socatServerPods
}

func (ct *ConnectivityTest) SocatClientPods() []Pod {
	return ct.socatClientPods
}

func (ct *ConnectivityTest) EchoPods() map[string]Pod {
	return ct.echoPods
}

func (ct *ConnectivityTest) LrpClientPods() map[string]Pod {
	return ct.lrpClientPods
}

func (ct *ConnectivityTest) LrpBackendPods() map[string]Pod {
	return ct.lrpBackendPods
}

// EchoServices returns all the non headless services
func (ct *ConnectivityTest) EchoServices() map[string]Service {
	svcs := map[string]Service{}
	for name, svc := range ct.echoServices {
		if svc.Service.Spec.ClusterIP == corev1.ClusterIPNone {
			continue
		}
		svcs[name] = svc
	}
	return svcs
}

func (ct *ConnectivityTest) EchoServicesAll() map[string]Service {
	return ct.echoServices
}

func (ct *ConnectivityTest) EchoExternalServices() map[string]Service {
	return ct.echoExternalServices
}

func (ct *ConnectivityTest) ExternalEchoPods() map[string]Pod {
	return ct.echoExternalPods
}

func (ct *ConnectivityTest) FRRPods() []Pod {
	return ct.frrPods
}

func (ct *ConnectivityTest) IngressService() map[string]Service {
	return ct.ingressService
}

func (ct *ConnectivityTest) L7LBService() map[string]Service {
	return ct.l7LBService
}

func (ct *ConnectivityTest) K8sService() Service {
	return ct.k8sService
}

func (ct *ConnectivityTest) HubbleClient() observer.ObserverClient {
	return ct.hubbleClient
}

func (ct *ConnectivityTest) PrintFlows() bool {
	return ct.params.PrintFlows
}

func (ct *ConnectivityTest) AllFlows() bool {
	return ct.params.AllFlows
}

func (ct *ConnectivityTest) FlowAggregation() bool {
	return ct.Features[features.MonitorAggregation].Enabled
}

func (ct *ConnectivityTest) PostTestSleepDuration() time.Duration {
	return ct.params.PostTestSleepDuration
}

func (ct *ConnectivityTest) K8sClient() *k8s.Client {
	return ct.client
}

func (ct *ConnectivityTest) NodesWithoutCilium() []string {
	return slices.Collect(maps.Keys(ct.nodesWithoutCilium))
}

func (ct *ConnectivityTest) Feature(f features.Feature) (features.Status, bool) {
	s, ok := ct.Features[f]
	return s, ok
}

func (ct *ConnectivityTest) Clients() []*k8s.Client {
	return ct.clients.clients()
}

func (ct *ConnectivityTest) InternalNodeIPAddresses(ipFamily features.IPFamily) []netip.Addr {
	var res []netip.Addr
	for _, node := range ct.Nodes() {
		for _, addr := range node.Status.Addresses {
			if addr.Type != slimcorev1.NodeInternalIP {
				continue
			}
			a, err := netip.ParseAddr(addr.Address)
			if err != nil {
				continue
			}
			if (ipFamily == features.IPFamilyV4 && a.Is4()) || (ipFamily == features.IPFamilyV6 && a.Is6()) {
				res = append(res, a)
			}
		}
	}
	return res
}

func (ct *ConnectivityTest) PodCIDRPrefixes(ipFamily features.IPFamily) []netip.Prefix {
	var res []netip.Prefix
	for _, node := range ct.Nodes() {
		for _, cidr := range node.Spec.PodCIDRs {
			p, err := netip.ParsePrefix(cidr)
			if err != nil {
				continue
			}
			if (ipFamily == features.IPFamilyV4 && p.Addr().Is4()) || (ipFamily == features.IPFamilyV6 && p.Addr().Is6()) {
				res = append(res, p)
			}
		}
	}
	return res
}

func (ct *ConnectivityTest) EchoServicePrefixes(ipFamily features.IPFamily) []netip.Prefix {
	var res []netip.Prefix
	for _, svc := range ct.EchoServices() {
		addr, err := netip.ParseAddr(svc.Address(ipFamily))
		if err == nil {
			res = append(res, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return res
}

// Multicast packet sender
// This command exits with exit code 0
// WITHOUT waiting for a second after receiving a packet.
func (ct *ConnectivityTest) SocatServer1secCommand(peer TestPeer, port int, group string) []string {
	addr := peer.Address(features.IPFamilyV4)
	cmdStr := fmt.Sprintf("timeout 5 socat STDIO UDP4-RECVFROM:%d,ip-add-membership=%s:%s", port, group, addr)
	cmd := strings.Fields(cmdStr)
	return cmd
}

// Multicast packet receiver
func (ct *ConnectivityTest) SocatClientCommand(port int, group string) []string {
	portStr := fmt.Sprintf("%d", port)
	cmdStr := fmt.Sprintf(`for i in $(seq 1 10000); do echo "%s" | socat - UDP-DATAGRAM:%s:%s; sleep 0.1; done`, socatMulticastTestMsg, group, portStr)
	cmd := []string{"/bin/sh", "-c", cmdStr}
	return cmd
}

func (ct *ConnectivityTest) KillMulticastTestSender() []string {
	cmd := []string{"pkill", "-f", socatMulticastTestMsg}
	return cmd
}

func (ct *ConnectivityTest) ForEachIPFamily(hasNetworkPolicies bool, do func(features.IPFamily)) {
	ipFams := features.GetIPFamilies(ct.Params().IPFamilies)

	for _, ipFam := range ipFams {
		switch ipFam {
		case features.IPFamilyV4:
			if f, ok := ct.Features[features.IPv4]; ok && f.Enabled {
				do(ipFam)
			}

		case features.IPFamilyV6:
			if f, ok := ct.Features[features.IPv6]; ok && f.Enabled {
				do(ipFam)
			}
		}
	}
}

func (ct *ConnectivityTest) ShouldRunConnDisruptNSTraffic() bool {
	return ct.params.IncludeConnDisruptTestNSTraffic &&
		ct.Features[features.NodeWithoutCilium].Enabled &&
		(ct.Params().MultiCluster == "" || ct.Features[features.KPRNodePort].Enabled) &&
		!ct.Features[features.KPRNodePortAcceleration].Enabled
}

func (ct *ConnectivityTest) ShouldRunConnDisruptEgressGateway() bool {
	return ct.params.IncludeUnsafeTests &&
		ct.params.IncludeConnDisruptTestEgressGateway &&
		ct.Features[features.EgressGateway].Enabled &&
		ct.Features[features.NodeWithoutCilium].Enabled &&
		!ct.Features[features.KPRNodePortAcceleration].Enabled &&
		ct.params.MultiCluster == ""
}

func (ct *ConnectivityTest) IsSocketLBFull() bool {
	socketLBEnabled, _ := ct.Features.MatchRequirements(features.RequireEnabled(features.KPRSocketLB))
	if socketLBEnabled {
		socketLBHostnsOnly, _ := ct.Features.MatchRequirements(features.RequireEnabled(features.KPRSocketLBHostnsOnly))
		return !socketLBHostnsOnly
	}
	return false
}

func (ct *ConnectivityTest) GetPodHostIPByFamily(pod Pod, ipFam features.IPFamily) (string, error) {
	for _, addr := range pod.Pod.Status.HostIPs {
		if features.GetIPFamily(addr.IP) == ipFam {
			return addr.IP, nil
		}
	}
	return "", fmt.Errorf("pod doesn't have HostIP of family %s", ipFam)
}
