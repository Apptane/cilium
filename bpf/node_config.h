/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
/* Copyright Authors of Cilium */

#pragma once

/*
 *   **** WARNING, THIS FILE IS DEPRECATED, DO NOT ADD NEW CONFIG HERE ****
 *
 * For adding new configuration to the datapath, see the documentation at
 * https://docs.cilium.io/en/latest/contributing/development/datapath_config.
 *
 * Variables in this file will gradually be migrated to the new format, and this
 * file will eventually be removed.
 */
#include <lib/static_data.h>

#define CLUSTER_ID 0

#define CILIUM_NET_IFINDEX 1
#define CILIUM_HOST_IFINDEX 1
#define NATIVE_DEV_MAC_BY_IFINDEX(_) { .addr = { 0xce, 0x72, 0xa7, 0x03, 0x88, 0x56 } }

#define LRU_MEM_FLAVOR 0

#define TUNNEL_PORT 8472
#define TUNNEL_PROTOCOL_VXLAN 1
#define TUNNEL_PROTOCOL_GENEVE 2
#ifndef TUNNEL_PROTOCOL
#define TUNNEL_PROTOCOL TUNNEL_PROTOCOL_VXLAN
#endif

#define UNKNOWN_ID 0
#define HOST_ID 1
#define WORLD_ID 2
#if defined ENABLE_IPV4 && defined ENABLE_IPV6
# define WORLD_IPV4_ID 9
# define WORLD_IPV6_ID 10
#else
# define WORLD_IPV4_ID 2
# define WORLD_IPV6_ID 2
#endif
#define UNMANAGED_ID 3
#define HEALTH_ID 4
#define INIT_ID 5
#define LOCAL_NODE_ID 6
#define REMOTE_NODE_ID 6
#define KUBE_APISERVER_NODE_ID 7
/* This identity should never be seen on ingress or egress traffic to/from a
 * node.
 * It signals that the skb is overlay traffic that must be IPSec encrypted
 * before it leaves the host.
 */
#define ENCRYPTED_OVERLAY_ID 11
#define CILIUM_HOST_MAC { .addr = { 0xce, 0x72, 0xa7, 0x03, 0x88, 0x56 } }
#define NODEPORT_PORT_MIN 30000
#define NODEPORT_PORT_MAX 32767
#define NODEPORT_PORT_MIN_NAT (NODEPORT_PORT_MAX + 1)
#define NODEPORT_PORT_MAX_NAT 65535

#define CT_CONNECTION_LIFETIME_TCP	21600
#define CT_CONNECTION_LIFETIME_NONTCP	60
#define CT_SERVICE_LIFETIME_TCP		21600
#define CT_SERVICE_LIFETIME_NONTCP	60
#define CT_SERVICE_CLOSE_REBALANCE	30
#define CT_SYN_TIMEOUT			60
#define CT_CLOSE_TIMEOUT		10
#define CT_REPORT_INTERVAL		5
#ifndef CT_REPORT_FLAGS
# define CT_REPORT_FLAGS		0xff
#endif

#define KERNEL_HZ 250   /* warp: 0 jiffies */

#define ENABLE_IDENTITY_MARK 1

#define HASH_INIT4_SEED 0xcafe
#define HASH_INIT6_SEED 0xeb9f

#ifndef L2_ANNOUNCEMENTS_MAX_LIVENESS
# define L2_ANNOUNCEMENTS_MAX_LIVENESS 3000000000ULL
#endif

#ifdef ENABLE_IPV4
#define IPV4_MASK 0xffff
#define IPV4_GATEWAY 0xfffff50a
#define IPV4_ENCRYPT_IFACE 0xfffff50a
# ifdef ENABLE_MASQUERADE_IPV4
#  define IPV4_SNAT_EXCLUSION_DST_CIDR 0xffff0000
#  define IPV4_SNAT_EXCLUSION_DST_CIDR_LEN 16
# endif /* ENABLE_MASQUERADE_IPV4 */
#ifdef ENABLE_NODEPORT
#if defined(ENABLE_CLUSTER_AWARE_ADDRESSING) && defined(ENABLE_INTER_CLUSTER_SNAT)
#define IPV4_INTER_CLUSTER_SNAT 0xfffff50a
#endif
#define SNAT_MAPPING_IPV4_SIZE 524288
#define NODEPORT_NEIGH4_SIZE 524288
#endif /* ENABLE_NODEPORT */
#define CAPTURE4_SIZE 16384
#endif /* ENABLE_IPV4 */

#ifdef ENABLE_IPV6
# ifdef ENABLE_MASQUERADE_IPV6
#  define IPV6_SNAT_EXCLUSION_DST_CIDR      { .addr = { 0xfa, 0xce, 0xff, 0xff, 0xff, 0x0 } }
#  define IPV6_SNAT_EXCLUSION_DST_CIDR_MASK { .addr = { 0xff, 0xff, 0xff, 0xff, 0xff, 0x0 } }
# endif /* ENABLE_MASQUERADE_IPV6 */
#ifdef ENABLE_NODEPORT
#define SNAT_MAPPING_IPV6_SIZE 524288
#define NODEPORT_NEIGH6_SIZE 524288
#endif /* ENABLE_NODEPORT */
#define CAPTURE6_SIZE 16384
#endif /* ENABLE_IPV6 */

#ifdef ENABLE_NODEPORT
#define SNAT_COLLISION_RETRIES 32
#endif

#ifndef EVENTS_MAP_RATE_LIMIT
# define EVENTS_MAP_RATE_LIMIT 0
#endif
#define EVENTS_MAP_BURST_LIMIT 0
#define POLICY_STATS_MAP_SIZE 200
#define LB6_REVERSE_NAT_SK_MAP_SIZE 262144
#define LB4_REVERSE_NAT_SK_MAP_SIZE 262144

#define LB_MAGLEV_LUT_SIZE 32749
#define THROTTLE_MAP_SIZE 65536
#define ENABLE_ARP_RESPONDER
#define VTEP_MAP_SIZE 8
#define ENDPOINTS_MAP_SIZE 65536
#define METRICS_MAP_SIZE 65536
#define CILIUM_NET_MAC  { .addr = { 0xce, 0x72, 0xa7, 0x03, 0x88, 0x57 } }
#define CILIUM_LB_REV_NAT_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_SERVICE_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_BACKENDS_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_AFFINITY_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_REV_NAT_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_MAGLEV_MAP_MAX_ENTRIES	65536
#define CILIUM_LB_SKIP_MAP_MAX_ENTRIES		100
#define CILIUM_LB_ACT_MAP_MAX_ENTRIES	    65536
#define POLICY_MAP_SIZE 16384
#define AUTH_MAP_SIZE 512000
#define CONFIG_MAP_SIZE 256
#define IPCACHE_MAP_SIZE 512000
#define NODE_MAP_SIZE 16384
#define EGRESS_POLICY_MAP_SIZE 16384
#define SRV6_VRF_MAP_SIZE 16384
#define SRV6_POLICY_MAP_SIZE 16384
#define SRV6_SID_MAP_SIZE 16384
#define L2_RESPONDER_MAP4_SIZE 4096
#define L2_RESPONDER_MAP6_SIZE 4096
#define POLICY_PROG_MAP_SIZE ENDPOINTS_MAP_SIZE
#define CILIUM_IPV4_FRAG_MAP_MAX_ENTRIES 8192
#define CILIUM_IPV6_FRAG_MAP_MAX_ENTRIES 8192
#ifndef SKIP_DEBUG
#define LB_DEBUG
#endif
#ifndef MONITOR_AGGREGATION
#define MONITOR_AGGREGATION 5
#endif
#define MTU 1500
#define EPHEMERAL_MIN 32768
#if defined(ENABLE_NODEPORT) || defined(ENABLE_HOST_FIREWALL) || defined(ENABLE_NAT_46X64)
#define CONNTRACK_ACCOUNTING
#define POLICY_ACCOUNTING

#endif /* ENABLE_NODEPORT || ENABLE_HOST_FIREWALL */

#define CT_MAP_SIZE_TCP 4096
#define CT_MAP_SIZE_ANY 4096

#ifdef ENABLE_NODEPORT
# ifdef ENABLE_IPV4
#  ifndef IPV4_DIRECT_ROUTING
#   define IPV4_DIRECT_ROUTING 0
#  endif
#  define IPV4_RSS_PREFIX IPV4_DIRECT_ROUTING
#  define IPV4_RSS_PREFIX_BITS 32
# endif
# ifdef ENABLE_IPV6
#  ifndef IPV6_DIRECT_ROUTING
#   define IPV6_DIRECT_ROUTING { .addr = { 0x0, 0x0, 0x0, 0x0, 0x0, 0x0 } }
#  endif
#  define IPV6_RSS_PREFIX IPV6_DIRECT_ROUTING
#  define IPV6_RSS_PREFIX_BITS 128
# endif
#endif

#ifndef IS_L3_DEV
# define IS_L3_DEV(ifindex) false
#endif

#ifdef ENABLE_SRC_RANGE_CHECK
# define LB4_SRC_RANGE_MAP_SIZE	1000
# define LB6_SRC_RANGE_MAP_SIZE	1000
#endif

#ifndef LB_SELECTION
# define LB_SELECTION_RANDOM	1
# define LB_SELECTION_MAGLEV	2
# define LB_SELECTION_FIRST	3
# define LB_SELECTION		LB_SELECTION_RANDOM
#endif

#ifdef ENABLE_WIREGUARD
# define WG_IFINDEX	42
# define WG_PORT    51871
# ifdef ENCRYPTION_STRICT_MODE
#  define STRICT_IPV4_NET	0
#  define STRICT_IPV4_NET_SIZE	8
# endif
#endif

#ifdef ENABLE_VTEP
# define VTEP_MASK 0xffffff
#endif

#define VLAN_FILTER(ifindex, vlan_id) switch (ifindex) { \
case 116: \
switch (vlan_id) { \
case 4000: \
case 4001: \
return true; \
} \
break; \
case 117: \
switch (vlan_id) { \
case 4003: \
case 4004: \
case 4005: \
return true; \
} \
break; \
} \
return false;

#define CIDR_IDENTITY_RANGE_START ((1 << 24) + 1)
#define CIDR_IDENTITY_RANGE_END   ((1 << 24) + (1<<16) - 1)

#ifndef NAT_46X64_PREFIX_0
# define NAT_46X64_PREFIX_0 0
# define NAT_46X64_PREFIX_1 0
# define NAT_46X64_PREFIX_2 0
# define NAT_46X64_PREFIX_3 0
#endif

#ifndef __CLUSTERMESH_IDENTITY__
#define __CLUSTERMESH_IDENTITY__
#define CLUSTER_ID_MAX 255
#endif

#ifndef __CLUSTERMESH_HELPERS__
#define __CLUSTERMESH_HELPERS__
#define IDENTITY_LEN 16
#define IDENTITY_MAX 65535
#endif

/*
 *   **** WARNING, THIS FILE IS DEPRECATED, SEE COMMENT AT THE TOP ****
 */
