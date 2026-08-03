#!/usr/bin/env python3
"""Evaluate the datagram demo's pass gate against the running fleet.

Scrapes every gateway's /metrics and /api/v1/self_info over the published
loopback ports and checks, in order:

  1. applied config    the config the node is actually coding at, read back
                       from self_info rather than assumed from what was served
  2. authentication    every gateway completed a verified handshake with every
                       peer, and none was rejected
  3. datagram sessions paths_confirmed == peers_total on every gateway
  4. transport         symbols went out over the datagram hook and nothing took
                       the stream fallback
  5. delivery          every subscriber decoded every published message
  6. spans             the OTel trace log has traced generations and a
                       measurable end-to-end latency

Each check prints its own numbers and the first failure decides the exit code,
with a reason naming the gateway and the value that failed.

Usage:
  verify.py [--gateways 5] [--base-port 48131] [--expect-messages 8]
            [--spans out/otel-collector.log] [--parser ./parse_v2_1.py]
"""

import argparse
import json
import re
import subprocess
import sys
import urllib.error
import urllib.request

# The datagram path derives its shard size from the transport's plaintext
# budget, so this number is a property of the run and not a knob:
#
#   1422 transport default MaxPayload  (OPT_DATAGRAM_MAX_PAYLOAD left unset)
#   -192 engine.SymbolFramingOverhead
#   - 38 len("/eth2/<8 hex digest>/beacon_block/ssz_snappy"), the longest topic
#        the gateway declares it publishes on
#   - 16 k coefficient bytes           (= rlnc_shard_factor)
#   = 1176
#
# Asserting it is the regression test this demo exists for: a derived shard size
# that never reaches the RLNC engine leaves the node coding at the 64 byte
# protocol default. It also catches a config-proxy 404, which is otherwise
# invisible: the gateway keeps its built-in k=4 and lands on 1188 instead.
EXPECTED_MAX_SHARD_SIZE = 1176
EXPECTED_SHARD_FACTOR = 16
EXPECTED_PUBLISHER_MULTIPLIER = 2.5
EXPECTED_CLUSTER_ID = "datagram-demo"
EXPECTED_CHAIN = "hoodi"

SHARD_SIZE_DERIVATION = (
    "1422 (transport default MaxPayload, sized for a 1500 byte Ethernet MTU) "
    "- 192 (SymbolFramingOverhead) - 38 (longest declared publish topic) "
    "- 16 (k) = 1176"
)

BEACON_BLOCK_TOPIC_RE = re.compile(r"^/eth2/[0-9a-f]+/beacon_block/ssz_snappy$")

SAMPLE_RE = re.compile(r'^(?P<name>[a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(?P<labels>[^}]*)\})?\s+(?P<value>.+)$')
LABEL_RE = re.compile(r'(\w+)="((?:[^"\\]|\\.)*)"')


class Failure(Exception):
    """A gate item that did not hold. The message is the reported reason."""


def http_get(url, timeout=10):
    with urllib.request.urlopen(url, timeout=timeout) as resp:  # noqa: S310 - fixed loopback URLs
        return resp.read().decode("utf-8", "replace")


def parse_metrics(text):
    """Parse the Prometheus text exposition into [(name, labels, value)]."""
    out = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        m = SAMPLE_RE.match(line)
        if not m:
            continue
        labels = dict(LABEL_RE.findall(m.group("labels") or ""))
        try:
            value = float(m.group("value").split()[0])
        except ValueError:
            continue
        out.append((m.group("name"), labels, value))
    return out


def metric_sum(samples, name, **match):
    """Sum every sample of `name` whose labels contain all of `match`."""
    total = 0.0
    for sname, labels, value in samples:
        if sname != name:
            continue
        if all(labels.get(k) == v for k, v in match.items()):
            total += value
    return total


def beacon_block_sum(samples, name):
    """Sum `name` over the eth2 beacon_block topic only.

    The mump2p node also carries `mump2p_aggregated_messages`, which has nothing
    to do with the blocks this run publishes.
    """
    total = 0.0
    for sname, labels, value in samples:
        if sname != name:
            continue
        if BEACON_BLOCK_TOPIC_RE.match(labels.get("topic", "")):
            total += value
    return total


class Gateway:
    def __init__(self, name, port):
        self.name = name
        self.port = port
        self.info = None
        self.samples = None

    def scrape(self):
        base = f"http://127.0.0.1:{self.port}"
        try:
            self.info = json.loads(http_get(f"{base}/api/v1/self_info"))
        except (urllib.error.URLError, OSError, json.JSONDecodeError) as err:
            raise Failure(f"{self.name}: cannot read {base}/api/v1/self_info: {err}") from err
        try:
            self.samples = parse_metrics(http_get(f"{base}/metrics"))
        except (urllib.error.URLError, OSError) as err:
            raise Failure(f"{self.name}: cannot read {base}/metrics: {err}") from err

    # --- derived views -----------------------------------------------------

    @property
    def rlnc(self):
        return self.info.get("rlnc_config", {})

    @property
    def datagram(self):
        return self.info.get("datagram", {})

    @property
    def mump2p_peers(self):
        return self.info.get("mump2p", {}).get("total_peers", 0)

    @property
    def hook_sends(self):
        return metric_sum(self.samples, "mump2p_datagram_sends_total", path="hook")

    @property
    def fallback_sends(self):
        return metric_sum(self.samples, "mump2p_datagram_sends_total", path="fallback")

    @property
    def forward_drops(self):
        return metric_sum(self.samples, "mump2p_datagram_forward_drops_total")

    @property
    def ingress_drops(self):
        return metric_sum(self.samples, "mump2p_datagram_ingress_drops_total")

    @property
    def handshakes_ok(self):
        return metric_sum(
            self.samples, "mump2p_gateway_p2p_handshake_cluster_claim_total", result="authorized"
        )

    @property
    def handshakes_rejected(self):
        return metric_sum(
            self.samples, "mump2p_gateway_p2p_handshake_cluster_claim_total", result="rejected"
        )

    @property
    def published(self):
        return beacon_block_sum(self.samples, "mump2p_gateway_mump2p_published_messages_per_topic_total")

    @property
    def delivered(self):
        # Incremented by the mump2p pubsub tracer on full RLNC decode, upstream
        # of the staleness and dedup gates, so nothing downstream can mask a
        # decode that did happen.
        return beacon_block_sum(self.samples, "mump2p_mump2p_delivered_messages_count")


# --- checks ----------------------------------------------------------------


def check_config(gateways):
    print("\n[1/6] applied config (read back from self_info, not assumed)")
    hdr = f"  {'gateway':<9} {'cluster':<15} {'chain':<6} {'k':>3} {'mult':>5} {'shard':>6} {'prop':>5} {'self':>5}"
    print(hdr)
    for gw in gateways:
        r = gw.rlnc
        print(
            f"  {gw.name:<9} {gw.info.get('gateway_cluster_id',''):<15} {gw.info.get('chain',''):<6} "
            f"{int(r.get('rlnc_shard_factor', 0)):>3} {r.get('publisher_shard_multiplier', 0):>5} "
            f"{int(r.get('max_shard_size', 0)):>6} {str(gw.info.get('propagation_enabled')):>5} "
            f"{str(not gw.info.get('skip_messages_from_self')):>5}"
        )
    for gw in gateways:
        r = gw.rlnc
        if gw.info.get("gateway_cluster_id") != EXPECTED_CLUSTER_ID:
            raise Failure(
                f"{gw.name}: cluster_id is {gw.info.get('gateway_cluster_id')!r}, "
                f"expected {EXPECTED_CLUSTER_ID!r}; a cluster mismatch presents as an empty mesh, not as an auth error"
            )
        if gw.info.get("chain") != EXPECTED_CHAIN:
            raise Failure(f"{gw.name}: chain is {gw.info.get('chain')!r}, expected {EXPECTED_CHAIN!r}")
        if int(r.get("rlnc_shard_factor", 0)) != EXPECTED_SHARD_FACTOR:
            raise Failure(
                f"{gw.name}: applied rlnc_shard_factor is {r.get('rlnc_shard_factor')}, "
                f"expected {EXPECTED_SHARD_FACTOR}; the config proxy did not reach this node "
                f"(a config fetch failure is silent and falls back to the built-in k=4)"
            )
        if abs(float(r.get("publisher_shard_multiplier", 0)) - EXPECTED_PUBLISHER_MULTIPLIER) > 1e-6:
            raise Failure(
                f"{gw.name}: applied publisher_shard_multiplier is {r.get('publisher_shard_multiplier')}, "
                f"expected {EXPECTED_PUBLISHER_MULTIPLIER}"
            )
        if int(r.get("max_shard_size", 0)) != EXPECTED_MAX_SHARD_SIZE:
            raise Failure(
                f"{gw.name}: derived max_shard_size is {r.get('max_shard_size')}, expected "
                f"{EXPECTED_MAX_SHARD_SIZE} ({SHARD_SIZE_DERIVATION}). Either the MTU-derived size did "
                f"not reach the RLNC engine, or max_shard_size was pinned somewhere, or the config "
                f"proxy 404'd and left k at the built-in 4 (which lands on 1188)"
            )
        if not gw.info.get("propagation_enabled"):
            raise Failure(
                f"{gw.name}: propagation_enabled is false; the served key is `propagation_enabled`, "
                f"and a misnamed key lands on the Go zero value with no fallback"
            )
        if gw.info.get("skip_messages_from_self"):
            raise Failure(f"{gw.name}: skip_messages_from_self is true, expected exclude_self_messages=false")
    print("  OK: every gateway holds the served config, including the derived shard size")


def check_auth(gateways, expected_peers):
    print("\n[2/6] mutual authentication")
    total_ok = sum(gw.handshakes_ok for gw in gateways)
    total_rejected = sum(gw.handshakes_rejected for gw in gateways)
    required = len(gateways) * expected_peers
    for gw in gateways:
        print(f"  {gw.name:<9} authorized={int(gw.handshakes_ok):>3} rejected={int(gw.handshakes_rejected):>3}")
    print(f"  total authorized={int(total_ok)} (need >= {required}) rejected={int(total_rejected)}")
    if total_rejected > 0:
        raise Failure(
            f"{int(total_rejected)} handshakes were rejected on cluster binding; "
            f"every gateway must present a JWT whose cluster_ids contains {EXPECTED_CLUSTER_ID!r}"
        )
    if total_ok < required:
        raise Failure(
            f"only {int(total_ok)} verified handshakes across the fleet, need >= {required} "
            f"({len(gateways)} gateways x {expected_peers} peers)"
        )
    print("  OK: every gateway verified a peer JWT for every peer")


def check_sessions(gateways, expected_peers):
    print("\n[3/6] datagram sessions (keys bootstrapped over the authenticated control connection)")
    for gw in gateways:
        d = gw.datagram
        print(
            f"  {gw.name:<9} enabled={str(d.get('enabled')):<5} local={d.get('local_addr',''):<12} "
            f"paths_confirmed={d.get('paths_confirmed')}/{d.get('peers_total')} mesh_peers={gw.mump2p_peers}"
        )
    for gw in gateways:
        d = gw.datagram
        if not d.get("enabled"):
            raise Failure(f"{gw.name}: the datagram data plane is disabled (OPT_DATAGRAM_ENABLE)")
        if d.get("peers_total", 0) != expected_peers:
            raise Failure(
                f"{gw.name}: peers_total is {d.get('peers_total')}, expected {expected_peers}; "
                f"the mesh did not form fully"
            )
        if d.get("paths_confirmed") != d.get("peers_total"):
            raise Failure(
                f"{gw.name}: paths_confirmed={d.get('paths_confirmed')} != peers_total={d.get('peers_total')}; "
                f"a peer with no confirmed path silently takes the stream fallback"
            )
    print("  OK: every peer has a confirmed UDP path and a live session")


def check_transport(gateways):
    print("\n[4/6] transport: which path carried the symbols")
    for gw in gateways:
        print(
            f"  {gw.name:<9} sends[hook]={int(gw.hook_sends):>7} sends[fallback]={int(gw.fallback_sends):>5} "
            f"forward_drops={int(gw.forward_drops):>4} ingress_drops={int(gw.ingress_drops):>4}"
        )
    fleet_hook = sum(gw.hook_sends for gw in gateways)
    fleet_fallback = sum(gw.fallback_sends for gw in gateways)
    print(f"  fleet hook={int(fleet_hook)} fallback={int(fleet_fallback)}")
    for gw in gateways:
        if gw.fallback_sends > 0:
            raise Failure(
                f"{gw.name}: {int(gw.fallback_sends)} sends took the stream fallback "
                f"(forward_drops={int(gw.forward_drops)}); the datagram path did not carry all the traffic"
            )
    for gw in gateways:
        if gw.hook_sends <= 0:
            raise Failure(
                f"{gw.name}: sends[hook] is 0, so this node put nothing on the datagram path. "
                f"Delivery alone would not have caught this: the forwarder falls back silently"
            )
    print("  OK: every gateway sent over the datagram hook and none fell back to the stream path")


def check_delivery(gateways, expect_messages):
    print("\n[5/6] delivery")
    publishers = [gw for gw in gateways if gw.published > 0]
    if not publishers:
        raise Failure(
            "no gateway published anything to the mump2p mesh; the publisher never reached the CL ingress "
            "(the gateway subscribes to beacon_block only after an eth2 status handshake)"
        )
    if len(publishers) > 1:
        raise Failure(
            f"{len(publishers)} gateways published ({[g.name for g in publishers]}); "
            f"the demo expects exactly one publishing ingress"
        )
    publisher = publishers[0]
    published = int(publisher.published)
    subscribers = [gw for gw in gateways if gw is not publisher]
    print(f"  publisher {publisher.name} published {published} beacon_block messages to the mesh")
    for gw in subscribers:
        got = int(gw.delivered)
        rate = (100.0 * got / published) if published else 0.0
        print(f"  {gw.name:<9} decoded={got:>3}/{published:<3} ({rate:5.1f}%)")
    if expect_messages is not None and published != expect_messages:
        raise Failure(
            f"{publisher.name} published {published} messages, expected {expect_messages}; "
            f"blocks were dropped at the CL ingress before they reached the mesh"
        )
    for gw in subscribers:
        got = int(gw.delivered)
        if got != published:
            raise Failure(
                f"{gw.name}: decoded {got} of {published} published messages "
                f"(forward_drops={int(gw.forward_drops)}, ingress_drops={int(gw.ingress_drops)})"
            )
    print(f"  OK: all {len(subscribers)} subscribers decoded all {published} messages")
    return publisher, published


def check_spans(spans_path, parser_path):
    print("\n[6/6] OTel spans")
    if spans_path is None:
        print("  skipped: no --spans given")
        return
    try:
        out = subprocess.run(  # noqa: S603 - fixed local parser
            [sys.executable, parser_path, spans_path],
            capture_output=True, text=True, check=True,
        ).stdout
    except (subprocess.CalledProcessError, OSError) as err:
        raise Failure(f"could not run {parser_path} on {spans_path}: {err}") from err

    for line in out.splitlines():
        print(f"  {line}")

    m = re.search(r"^traces \(published generations\): (\d+)$", out, re.M)
    if not m:
        raise Failure(f"{parser_path} reported no traced-generation count; the span log has no rlnc.publish spans")
    traced = int(m.group(1))
    if traced == 0:
        raise Failure(
            "0 traced generations: the collector received no publish spans. Check OPT_OTEL_ENDPOINT "
            "(host:port, no scheme) and that the collector uses the debug exporter at verbosity: detailed"
        )
    if not re.search(r"^END-TO-END latency ", out, re.M):
        raise Failure(
            f"{traced} traced generations but no end-to-end latency line: no receiver completed "
            f"every chunk of a generation whose publish span was also captured"
        )
    print(f"  OK: {traced} traced generations with a measurable end-to-end latency")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--gateways", type=int, default=5)
    ap.add_argument("--base-port", type=int, default=48131, help="host port of gateway1; the rest follow")
    ap.add_argument("--expect-messages", type=int, default=None, help="blocks the publisher was asked to send")
    ap.add_argument("--spans", default=None, help="path to the captured otel-collector log")
    ap.add_argument("--parser", default="./parse_v2_1.py", help="span parser to run over --spans")
    args = ap.parse_args()

    gateways = [Gateway(f"gateway{i}", args.base_port + i - 1) for i in range(1, args.gateways + 1)]
    expected_peers = args.gateways - 1

    print(f"datagram demo verification: {args.gateways} gateways on ports "
          f"{args.base_port}..{args.base_port + args.gateways - 1}")

    try:
        for gw in gateways:
            gw.scrape()
    except Failure as err:
        print(f"\nFAIL: {err}")
        return 1

    # Every check is run even after one fails, and each reports its own numbers.
    # A single first-failure would hide, for instance, that a run with the data
    # plane off fails the transport assertion as well as the session one, which
    # is exactly what makes the transport claim non-vacuous.
    checks = [
        ("applied config", lambda: check_config(gateways)),
        ("mutual authentication", lambda: check_auth(gateways, expected_peers)),
        ("datagram sessions", lambda: check_sessions(gateways, expected_peers)),
        ("transport", lambda: check_transport(gateways)),
        ("delivery", lambda: check_delivery(gateways, args.expect_messages)),
        ("OTel spans", lambda: check_spans(args.spans, args.parser)),
    ]
    failures = []
    for label, fn in checks:
        try:
            fn()
        except Failure as err:
            print(f"  FAIL: {err}")
            failures.append((label, str(err)))

    if failures:
        print(f"\nFAIL: {len(failures)} gate item(s) did not hold")
        for label, reason in failures:
            print(f"  - {label}: {reason}")
        return 1

    print("\nPASS: RLNC traffic crossed an encrypted UDP data plane whose keys were "
          "bootstrapped over the authenticated control connection, with full delivery.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
