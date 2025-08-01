.. only:: not (epub or latex or html)

    WARNING: You are looking at unreleased Cilium documentation.
    Please use the official rendered version released here:
    https://docs.cilium.io

.. _gs_l7_traffic_management:

***************************
L7-Aware Traffic Management
***************************

Cilium provides a way to control L7 traffic via CRDs (e.g. CiliumEnvoyConfig
and CiliumClusterwideEnvoyConfig).

Prerequisites
#############

* Cilium must be configured with NodePort enabled, using
  ``nodePort.enabled=true`` or by enabling the kube-proxy replacement with
  ``kubeProxyReplacement=true``. For more information, see :ref:`kube-proxy
  replacement <kubeproxy-free>`.

Caveats
#######

* ``CiliumEnvoyConfig`` resources have only minimal validation performed, and
  do not have a defined conflict resolution behavior. This means that if you
  create multiple CECs that modify the same parts of Envoy's config, the results
  may be unpredictable.
* In addition to this minimal validation, ``CiliumEnvoyConfig`` has minimal
  feedback to the user about the correctness of the configuration. So in the
  event a CEC does produce an undesirable outcome, troubleshooting will require
  inspecting the Envoy config and logs, rather than being able to look at the
  ``CiliumEnvoyConfig`` in question.
* ``CiliumEnvoyConfig`` is used by Cilium's Ingress and Gateway API support to
  direct traffic through the per-node Envoy proxies. If you create CECs that
  conflict with or modify the autogenerated config, results may be unpredictable.
  Be very careful using CECs for these use cases. The above risks are managed
  by ensuring that all config generated by Cilium is semantically valid, as far
  as possible.
* If you create a ``CiliumEnvoyConfig`` resource directly (ie, not via the
  Cilium Ingress or Gateway API controllers), if the CEC is intended to manage
  E/W traffic, set the annotation ``cec.cilium.io/use-original-source-address: "false"``.
  Otherwise, Envoy will bind the sockets for the upstream connection pools to
  the original source address/port. This may cause 5-tuple collisions when pods
  send multiple requests over the same pipelined HTTP/1.1 or HTTP/2 connection.
  (The Cilium agent assumes all CECs with parentRefs pointing to the Cilium
  Ingress or Gateway API controllers have annotation
  ``cec.cilium.io/use-original-source-address`` set to ``"false"``, but all other CECs
  are assumed to have this annotation set to ``"true"``.)

.. include:: installation.rst

Supported Envoy API Versions
============================

As of now only the Envoy API v3 is supported.

Supported Envoy Extension Resource Types
========================================

Envoy extensions are resource types that may or may not be built in to
an Envoy build. The standard types referred to in Envoy documentation,
such as ``type.googleapis.com/envoy.config.listener.v3.Listener``, and
``type.googleapis.com/envoy.config.route.v3.RouteConfiguration``, are
always available.

Cilium nodes deploy an Envoy image to support Cilium HTTP policy
enforcement and observability. This build of Envoy has been optimized
for the needs of the Cilium Agent and does not contain many of the
Envoy extensions available in the Envoy code base.

To see which Envoy extensions are available, please have a look at
the `Envoy extensions configuration
file <https://github.com/cilium/proxy/blob/main/envoy_build_config/extensions_build_config.bzl>`_.
Only the extensions that have not been commented out with ``#`` are
built in to the Cilium Envoy image. We will evolve the list of built-in
extensions based on user feedback.

Examples
########

Please refer to one of the below examples on how to use and leverage
Cilium's Ingress features:

.. toctree::
   :maxdepth: 1
   :glob:

   envoy-custom-listener
   envoy-traffic-management
   envoy-circuit-breaker
   envoy-load-balancing
   envoy-traffic-shifting
