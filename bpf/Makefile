# Copyright Authors of Cilium
# SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)

include ../Makefile.defs

SUBDIRS = custom
BPF_SIMPLE = bpf_network.o bpf_alignchecker.o
BPF = bpf_lxc.o bpf_overlay.o bpf_sock.o bpf_host.o bpf_wireguard.o bpf_xdp.o $(BPF_SIMPLE)

KERNEL ?= netnext

BPF_SIMPLE_OPTIONS ?= \
	-DENABLE_IPV4=1 -DENABLE_IPV6=1 -DENABLE_IPSEC=1

# The following option combinations are compile tested
LB_OPTIONS = \
	-DSKIP_DEBUG: \
	-DENABLE_IPV4: \
	-DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE: \
	-DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_IPV4_FRAGMENTS: \
	-DENABLE_IPV6: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_L7_LB: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_L7_LB:-DENABLE_EGRESS_GATEWAY:-DENABLE_MASQUERADE_IPV4:-DENABLE_IP_MASQ_AGENT_IPV4:-DENABLE_MASQUERADE_IPV6:-DENABLE_IP_MASQ_AGENT_IPV6: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_L7_LB:-DENABLE_EGRESS_GATEWAY_COMMON:-DENABLE_MASQUERADE_IPV4:-DENABLE_MASQUERADE_IPV6: \
	-DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY: \
	-DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_L7_LB: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_SRC_RANGE_CHECK: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_RANDOM: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV:-DENABLE_SOCKET_LB_HOST_ONLY: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV:-DENABLE_SOCKET_LB_HOST_ONLY:-DENABLE_L7_LB:-DENABLE_SCTP: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV:-DENABLE_SOCKET_LB_HOST_ONLY:-DENABLE_L7_LB:-DENABLE_SCTP:-DENABLE_VTEP: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DDSR_ENCAP_MODE:-DDSR_ENCAP_GENEVE:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV:-DENABLE_SOCKET_LB_HOST_ONLY:-DENABLE_L7_LB:-DENABLE_SCTP:-DDSR_ENCAP_IPIP=2 \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_SESSION_AFFINITY:-DENABLE_BANDWIDTH_MANAGER:-DENABLE_SRC_RANGE_CHECK:-DLB_SELECTION:-DLB_SELECTION_MAGLEV:-DENABLE_SOCKET_LB_HOST_ONLY:-DENABLE_L7_LB:-DENABLE_SCTP:-DENABLE_VTEP:-DENABLE_CLUSTER_AWARE_ADDRESSING:-DENABLE_INTER_CLUSTER_SNAT:

# These options are intended to max out the BPF program complexity. it is load
# tested as well.
MAX_BASE_OPTIONS = -DSKIP_DEBUG=1 -DENABLE_IPV4=1 -DENABLE_IPV6=1 \
	-DENABLE_ROUTING=1 \
	-DPOLICY_VERDICT_NOTIFY=1 -DALLOW_ICMP_FRAG_NEEDED=1 -DENABLE_IDENTITY_MARK=1 \
	-DMONITOR_AGGREGATION=3 -DCT_REPORT_FLAGS=0x0002 -DENABLE_HOST_FIREWALL=1 \
	-DENABLE_ICMP_RULE=1 -DENABLE_CUSTOM_CALLS=1 -DENABLE_SRV6=1 -DENABLE_L7_LB=1
MAX_BASE_OPTIONS += -DENABLE_MASQUERADE_IPV4=1 -DENABLE_IP_MASQ_AGENT_IPV4=1 \
	-DENABLE_MASQUERADE_IPV6=1 -DENABLE_IP_MASQ_AGENT_IPV6=1 \
	-DENABLE_SRC_RANGE_CHECK=1 -DENABLE_NODEPORT=1 \
	-DENABLE_NODEPORT_ACCELERATION=1 -DENABLE_SESSION_AFFINITY=1 \
	-DENABLE_DSR_ICMP_ERRORS=1 -DENABLE_DSR=1 -DENABLE_DSR_HYBRID=1 \
	-DENABLE_IPV4_FRAGMENTS=1
# Egress Gateway requires >= 5.2 kernels, bandwidth manager requires >= 5.1.
MAX_BASE_OPTIONS += -DENABLE_BANDWIDTH_MANAGER=1 -DENABLE_EGRESS_GATEWAY=1 -DENABLE_VTEP=1
ifneq ($(KERNEL),54)
# BPF TProxy requires 5.7, BPF Host routing 5.10, L3 devices 5.8.
MAX_BASE_OPTIONS += -DENABLE_TPROXY=1 -DENABLE_HOST_ROUTING=1
endif

ifndef MAX_LB_OPTIONS
MAX_LB_OPTIONS = $(MAX_BASE_OPTIONS) -DENABLE_NAT_46X64=1 -DENABLE_NAT_46X64_GATEWAY=1 -DENCAP_IFINDEX=1 -DTUNNEL_MODE=1
MAX_LB_OPTIONS += -DLB_SELECTION_PER_SERVICE=1 -DLB_SELECTION_MAGLEV=2 -DLB_SELECTION_RANDOM=1 -DENABLE_IPSEC=1
endif

ifndef MAX_OVERLAY_OPTIONS
MAX_OVERLAY_OPTIONS = $(MAX_BASE_OPTIONS) -DENCAP_IFINDEX=1 -DTUNNEL_MODE=1 -DENABLE_MULTICAST=1
MAX_OVERLAY_OPTIONS += -DLB_SELECTION_PER_SERVICE=1 -DLB_SELECTION_MAGLEV=2 -DLB_SELECTION_RANDOM=1 -DENABLE_IPSEC=1
endif

HOST_OPTIONS = $(LXC_OPTIONS) \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_HOST_FIREWALL: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_HOST_FIREWALL: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_DSR: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC:-DENABLE_DSR: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_IP_MASQ_AGENT_IPV4:-DENABLE_MASQUERADE_IPV6:-DENABLE_IP_MASQ_AGENT_IPV6: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_MASQUERADE_IPV6:-DENABLE_EGRESS_GATEWAY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_MASQUERADE_IPV6:-DENABLE_EGRESS_GATEWAY_COMMON: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_MASQUERADE_IPV6: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_DSR: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_DSR_HYBRID: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_HOST_FIREWALL: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_HOST_FIREWALL: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE: \
	-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_MULTICAST:-DENCRYPTED_OVERLAY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_MULTICAST:-DENCRYPTED_OVERLAY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_SRV6_SRH_ENCAP:-DENABLE_SCTP:-DENABLE_MULTICAST:-DENCRYPTED_OVERLAY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_SRV6_SRH_ENCAP:-DENABLE_SCTP:-DENABLE_VTEP:-DENABLE_MULTICAST:-DENCRYPTED_OVERLAY: \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DDSR_ENCAP_MODE:-DDSR_ENCAP_GENEVE:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_SRV6_SRH_ENCAP:-DENCRYPTED_OVERLAY:-DENABLE_SCTP:-DENABLE_VTEP:-DDSR_ENCAP_IPIP=2 \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NODEPORT:-DENABLE_NODEPORT_ACCELERATION:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DENABLE_PREFILTER:-DENABLE_SESSION_AFFINITY:-DENABLE_HOST_FIREWALL:-DENABLE_ICMP_RULE:-DENABLE_SRV6:-DENABLE_SRV6_SRH_ENCAP:-DENCRYPTED_OVERLAY:-DENABLE_SCTP:-DENABLE_VTEP:-DENABLE_IPSEC: \

ifndef MAX_HOST_OPTIONS
MAX_HOST_OPTIONS = $(MAX_BASE_OPTIONS) -DENCAP_IFINDEX=1 -DTUNNEL_MODE=1 -DENABLE_IPSEC=1

ifneq ($(KERNEL),54)
MAX_HOST_OPTIONS += -DENABLE_WIREGUARD=1 -DENCRYPTION_STRICT_MODE=1
endif
endif

WIREGUARD_OPTIONS = $(HOST_OPTIONS)

ifndef MAX_WIREGUARD_OPTIONS
MAX_WIREGUARD_OPTIONS = $(MAX_BASE_OPTIONS) -DENCAP_IFINDEX=1 -DTUNNEL_MODE=1 -DENABLE_WIREGUARD=1

ifneq ($(KERNEL),54)
MAX_WIREGUARD_OPTIONS += -DENABLE_WIREGUARD=1 -DENCRYPTION_STRICT_MODE=1
endif
endif

XDP_OPTIONS = $(LB_OPTIONS) \
	-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_DSR:-DFROM_HOST: \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_IP_MASQ_AGENT_IPV4:-DENABLE_MASQUERADE_IPV6:-DENABLE_IP_MASQ_AGENT_IPV6: \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_MASQUERADE_IPV4:-DENABLE_MASQUERADE_IPV6: \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR: \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_DSR_HYBRID: \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DTUNNEL_MODE:-DTUNNEL_PROTOCOL=TUNNEL_PROTOCOL_VXLAN \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_DSR_HYBRID:-DTUNNEL_MODE:-DTUNNEL_PROTOCOL=TUNNEL_PROTOCOL_GENEVE \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DDSR_ENCAP_MODE:-DDSR_ENCAP_NONE:-DDSR_ENCAP_IPIP=2 \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DDSR_ENCAP_MODE:-DDSR_ENCAP_IPIP:-DDSR_ENCAP_NONE=2 \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DDSR_ENCAP_MODE:-DDSR_ENCAP_GENEVE:-DDSR_ENCAP_IPIP=2 \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_CAPTURE:-DDSR_ENCAP_MODE:-DDSR_ENCAP_NONE:-DDSR_ENCAP_IPIP=2 \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_CAPTURE:-DDSR_ENCAP_MODE:-DDSR_ENCAP_IPIP:-DENABLE_SCTP:-DDSR_ENCAP_NONE=2 \
	-DENABLE_NODEPORT_ACCELERATION:-DENABLE_IPV4:-DENABLE_IPV6:-DENABLE_NODEPORT:-DENABLE_DSR:-DENABLE_CAPTURE:-DDSR_ENCAP_MODE:-DDSR_ENCAP_GENEVE:-DENABLE_SCTP:-DDSR_ENCAP_IPIP=2

ifndef MAX_XDP_OPTIONS
MAX_XDP_OPTIONS = $(MAX_BASE_OPTIONS) -DENABLE_PREFILTER=1
MAX_XDP_OPTIONS += -DLB_SELECTION_PER_SERVICE=1 -DLB_SELECTION_MAGLEV=2 -DLB_SELECTION_RANDOM=1
endif

# The following option combinations are compile tested
LXC_OPTIONS = \
	 -DALLOW_ICMP_FRAG_NEEDED: \
	 -DSKIP_DEBUG: \
	 -DENABLE_IPV4: \
	 -DENABLE_IPV6: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC: \
	 -DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY: \
	 -DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC: \
	 -DENABLE_IPV4:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPV4_FRAGMENTS: \
	 -DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY: \
	 -DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_ROUTING: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_IPSEC:-DENABLE_L7_LB: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_TPROXY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_TPROXY:-DENABLE_L7_LB: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DENABLE_NAT_46X64:-DENABLE_NAT_46X64_GATEWAY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DPOLICY_VERDICT_NOTIFY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_NODEPORT:-DENABLE_DSR:-DPOLICY_VERDICT_NOTIFY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DPOLICY_VERDICT_NOTIFY:-DUSE_BPF_PROG_FOR_INGRESS_POLICY: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING:-DENABLE_SKIP_FIB: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING:-DENABLE_SKIP_FIB:-DENABLE_ICMP_RULE:-DENABLE_SCTP: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DTUNNEL_MODE:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING:-DENABLE_SKIP_FIB:-DENABLE_ICMP_RULE:-DENABLE_SCTP:-DENABLE_VTEP: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING:-DENABLE_SKIP_FIB:-DENABLE_ICMP_RULE:-DENABLE_SCTP: \
	 -DENABLE_IPV4:-DENABLE_IPV6:-DENCAP_IFINDEX:-DENABLE_TPROXY:-DENABLE_HOST_ROUTING:-DENABLE_SKIP_FIB:-DENABLE_ICMP_RULE:-DENABLE_SCTP:-DENABLE_IPSEC: \

# These options are intended to max out the BPF program complexity. it is load
# tested as well.
ifndef MAX_LXC_OPTIONS
MAX_LXC_OPTIONS = $(MAX_BASE_OPTIONS) -DENCAP_IFINDEX=1 -DTUNNEL_MODE=1 -DENABLE_IPSEC=1
endif

# Add the ability to override variables
-include Makefile.override

include ./Makefile.bpf

.PHONY: all bpf_all build_all subdirs install clean generate gen_compile_commands

all: bpf_all

bpf_all: $(BPF) subdirs generate

build_all: force
	@$(ECHO_CHECK)/*.c BUILD_PERMUTATIONS=1
	$(QUIET) $(MAKE) $(SUBMAKEOPTS) bpf_all BUILD_PERMUTATIONS=1

testdata:
	${CLANG} ${FLAGS} --target=bpf -Wall -Werror \
	-c ../pkg/alignchecker/testdata/bpf_foo.c \
	-o ../pkg/alignchecker/testdata/bpf_foo.o

generate: $(BPF)
	$(GO) generate ../pkg/datapath/config

$(BPF_SIMPLE): %.o: %.c
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${BPF_SIMPLE_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

# Hack to get make to replace : with a space
null :=
space := ${null} ${null}

ifneq ($(BUILD_PERMUTATIONS),)
define PERMUTATION_template =
$(1)::
	@$$(ECHO_CC) " [$(subst :,=1$(space),$(2))]"
	$$(QUIET) $${CLANG} $(subst :,=1$(space),$(2)) $${CLANG_FLAGS} -c $(patsubst %.o,%.c,$(1)) -o $(1)
endef
endif

bpf_sock.o:: bpf_sock.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_LB_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(LB_OPTIONS),$(eval $(call PERMUTATION_template,bpf_sock.o,$(OPTS))))

bpf_overlay.o:: bpf_overlay.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_OVERLAY_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(LB_OPTIONS),$(eval $(call PERMUTATION_template,bpf_overlay.o,$(OPTS) -DENCAP_IFINDEX=1)))

bpf_host.o:: bpf_host.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_HOST_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(HOST_OPTIONS),$(eval $(call PERMUTATION_template,bpf_host.o,$(OPTS) -DENCAP_IFINDEX=1)))

bpf_wireguard.o:: bpf_wireguard.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_WIREGUARD_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(WIREGUARD_OPTIONS),$(eval $(call PERMUTATION_template,bpf_wireguard.o,$(OPTS) -DENCAP_IFINDEX=1)))

bpf_xdp.o:: bpf_xdp.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_XDP_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(XDP_OPTIONS),$(eval $(call PERMUTATION_template,bpf_xdp.o,$(OPTS))))

bpf_lxc.o:: bpf_lxc.c $(LIB)
	@$(ECHO_CC)
	$(QUIET) ${CLANG} ${MAX_LXC_OPTIONS} ${CLANG_FLAGS} -c $< -o $@

$(foreach OPTS,$(LXC_OPTIONS),$(eval $(call PERMUTATION_template,bpf_lxc.o,$(OPTS))))

subdirs: $(SUBDIRS)
	$(QUIET) $(foreach TARGET,$(SUBDIRS), \
		$(MAKE) $(SUBMAKEOPTS) -C $(TARGET) &&) true

install-binary:

install-bash-completion:

clean:
	@$(ECHO_CLEAN)
	$(QUIET) $(foreach TARGET,$(SUBDIRS), \
		$(MAKE) $(SUBMAKEOPTS) -C $(TARGET) clean;)
	$(QUIET)rm -fr *.o *.ll *.i *.s
	$(QUIET)rm -f $(TARGET)


BEAR_CLI   = $(shell which bear 2> /dev/null)
gen_compile_commands:
ifeq (, $(BEAR_CLI))
	@echo 'Bear cli must be in $$PATH to generate json compilation database'
else
	bear -- make
endif
