#!/usr/bin/env python3
"""Report what a mid-scale run actually did, rather than assert that it passed.

The five-gateway stack's verify.py answers "did everything work". This one
answers "how far did a generation get", because at this node count the expected
outcome is that it does not get far. Delivery is an observation here, not a gate:
the only things that fail the run are the ones that would make the observation
meaningless (an unapplied config, a mesh that never authenticated, sessions that
never confirmed, symbols that took the stream fallback).

What it reports, in order:

  1. applied config    read back from self_info, including the derived shard size
  2. authentication    verified handshakes, and rejections
  3. sessions          paths_confirmed vs peers_total
  4. topology          peers per gateway (P), which is the variable under study
  5. transport         hook vs fallback, and how many gateways ever sent at all
  6. delivery          decoded/published per subscriber
  7. rank              rank reached per receiver-chunk against the required k
  8. recode            rlnc.symbol.recode spans and the generations they covered
  9. symbols           helpful vs redundant vs unnecessary

Usage:
  report.py [--gateways 12] [--base-port 48151] [--expect-messages 8]
            [--spans out/otel-collector.log] [--json out/report.json]
"""

import argparse
import json
import re
import sys
import urllib.error
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone

EXPECTED_CLUSTER_ID = "datagram-scale"
EXPECTED_CHAIN = "hoodi"

# The datagram path derives its shard size from the transport's plaintext
# budget, so this is a property of the run rather than a knob:
#
#   1382 transport default MaxPayload (OPT_DATAGRAM_MAX_PAYLOAD left unset)
#   -192 engine.SymbolFramingOverhead
#   - 38 len("/eth2/<8 hex digest>/beacon_block/ssz_snappy"), the longest topic
#   -  k coefficient bytes
#
# Checking it catches a config-proxy 404, which is otherwise invisible: the
# gateway keeps its built-in k=4 and every number below shifts with it.
SHARD_BUDGET = 1382 - 192 - 38

BEACON_BLOCK_TOPIC_RE = re.compile(r"^/eth2/[0-9a-f]+/beacon_block/ssz_snappy$")
SAMPLE_RE = re.compile(r'^(?P<name>[a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(?P<labels>[^}]*)\})?\s+(?P<value>.+)$')
LABEL_RE = re.compile(r'(\w+)="((?:[^"\\]|\\.)*)"')


class Broken(Exception):
    """A precondition that makes the run's numbers meaningless."""


def http_get(url, timeout=10):
    with urllib.request.urlopen(url, timeout=timeout) as resp:  # noqa: S310 - fixed loopback URLs
        return resp.read().decode("utf-8", "replace")


def parse_metrics(text):
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
    return sum(v for n, lb, v in samples
               if n == name and all(lb.get(k) == want for k, want in match.items()))


def beacon_block_sum(samples, name):
    """Sum `name` over the eth2 beacon_block topic only.

    The node also carries `mump2p_aggregated_messages`, which has nothing to do
    with the blocks this run publishes.
    """
    return sum(v for n, lb, v in samples
               if n == name and BEACON_BLOCK_TOPIC_RE.match(lb.get("topic", "")))


def pct(xs, p):
    xs = sorted(xs)
    if not xs:
        return 0
    return xs[min(len(xs) - 1, int(round(p / 100 * (len(xs) - 1))))]


class Gateway:
    def __init__(self, name, port):
        self.name = name
        self.port = port
        self.info = {}
        self.samples = []

    def scrape(self):
        base = f"http://127.0.0.1:{self.port}"
        try:
            self.info = json.loads(http_get(f"{base}/api/v1/self_info"))
        except (urllib.error.URLError, OSError, json.JSONDecodeError) as err:
            raise Broken(f"{self.name}: cannot read {base}/api/v1/self_info: {err}") from err
        try:
            self.samples = parse_metrics(http_get(f"{base}/metrics"))
        except (urllib.error.URLError, OSError) as err:
            raise Broken(f"{self.name}: cannot read {base}/metrics: {err}") from err

    @property
    def rlnc(self):
        return self.info.get("rlnc_config", {})

    @property
    def datagram(self):
        return self.info.get("datagram", {})

    @property
    def peers(self):
        return self.info.get("mump2p", {}).get("total_peers", 0)

    @property
    def node_ids(self):
        """Every identifier this gateway's spans could be tagged with."""
        ids = set()
        for _, labels, _ in self.samples:
            for key in ("node_id", "local_peer_id"):
                if labels.get(key):
                    ids.add(labels[key])
        return ids

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
        return metric_sum(self.samples, "mump2p_gateway_p2p_handshake_cluster_claim_total",
                          result="authorized")

    @property
    def handshakes_rejected(self):
        return metric_sum(self.samples, "mump2p_gateway_p2p_handshake_cluster_claim_total",
                          result="rejected")

    @property
    def published(self):
        return beacon_block_sum(self.samples, "mump2p_gateway_mump2p_published_messages_per_topic_total")

    @property
    def delivered(self):
        # Incremented by the pubsub tracer on full RLNC decode, upstream of the
        # staleness and dedup gates, so nothing downstream can mask a decode.
        return beacon_block_sum(self.samples, "mump2p_mump2p_delivered_messages_count")


# --- spans -----------------------------------------------------------------

LINE = re.compile(r'stderr F (.*)$')
TS = re.compile(r'(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:\.\d+)?) \+0000 UTC')
RECV_PREFIXES = ('rlnc.shard.recv.', 'rlnc.symbol.recv.')
RECODE_NAMES = ('rlnc.shard.recode', 'rlnc.symbol.recode')


def parse_ns(s):
    m = TS.search(s)
    if not m:
        return None
    t = m.group(1)
    base, frac = (t.split('.') + ['0'])[:2] if '.' in t else (t, '0')
    frac = (frac + '000000000')[:9]
    dt = datetime.strptime(base, '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
    return int(dt.timestamp()) * 1_000_000_000 + int(frac)


def parse_spans(path):
    """Parse the collector's debug-exporter dump into a flat list of spans.

    Format is the collector's own text rendering: a `Resource attributes:` block
    carrying mump2p.node_id, then one `Span #n` block per span.
    """
    spans, cur, cur_node = [], None, None

    def flush():
        nonlocal cur
        if cur and cur.get('name'):
            cur['node'] = cur_node
            spans.append(cur)
        cur = None

    with open(path, errors="replace") as fh:
        for raw in fh:
            m = LINE.search(raw)
            s = (m.group(1) if m else raw).strip()
            if s.startswith('Resource attributes:'):
                flush()
                continue
            if s.startswith('-> mump2p.node_id:'):
                m = re.search(r'Str\((.*)\)', s)
                if m:
                    cur_node = m.group(1)
                continue
            if s.startswith('-> service.instance.id:') and cur_node is None:
                m = re.search(r'Str\((.*)\)', s)
                if m:
                    cur_node = m.group(1)
                continue
            if s.startswith('Span #'):
                flush()
                cur = {}
                continue
            if cur is None:
                continue
            if s.startswith('Trace ID'):
                m = re.search(r':\s*([0-9a-f]+)', s)
                cur['trace'] = m.group(1) if m else None
            elif s.startswith('Name'):
                cur['name'] = s.split(':', 1)[1].strip()
            elif s.startswith('Start time'):
                cur['start'] = parse_ns(s)
            elif s.startswith('End time'):
                cur['end'] = parse_ns(s)
            elif s.startswith('-> rlnc.symbol.validity:'):
                m = re.search(r'Str\((.*)\)', s)
                cur['validity'] = m.group(1) if m else None
            elif s.startswith('-> rlnc.received_from:'):
                m = re.search(r'Str\((.*)\)', s)
                cur['rfrom'] = m.group(1) if m else None
            elif s.startswith('-> rlnc.decode.completed:'):
                cur['completed'] = 'true' in s.lower()
            elif s.startswith('-> rlnc.chunk_id:'):
                m = re.search(r'Int\((\d+)\)', s)
                cur['chunk'] = int(m.group(1)) if m else 0
    flush()
    return [s for s in spans if s.get('name') and s.get('trace')]


class SpanView:
    """Per-(generation, node, chunk) view of one run's spans.

    A generation is one published message (one trace); a chunk is one RLNC
    generation within it. Rank is counted as the number of helpful symbols a node
    accepted for a chunk, which is exactly the quantity the forwarding gate
    compares against int(k*f).
    """

    def __init__(self, spans):
        self.spans = spans
        self.pub_traces = set()
        self.pub_nodes = set()
        self.helpful = defaultdict(int)     # (trace,node,chunk) -> rank
        self.redundant = defaultdict(int)
        self.unnecessary = defaultdict(int)
        self.sources = defaultdict(set)     # (trace,node,chunk) -> upstreams
        self.completed = {}                 # (trace,node,chunk) -> bool
        self.recode_spans = defaultdict(int)          # node -> span count
        self.recode_gens = defaultdict(set)           # node -> {(trace,chunk)}
        self.recode_chunk_ids = defaultdict(set)      # node -> {chunk}
        self.max_chunk = 0

        for s in spans:
            if s['name'] == 'rlnc.publish':
                self.pub_traces.add(s['trace'])
                self.pub_nodes.add(s['node'])

        for s in spans:
            chunk = s.get('chunk', 0) or 0
            key = (s['trace'], s['node'], chunk)
            name = s['name']
            if name == 'rlnc.decode':
                self.max_chunk = max(self.max_chunk, chunk)
                self.completed[key] = self.completed.get(key, False) or bool(s.get('completed'))
            elif name.startswith(RECV_PREFIXES):
                self.max_chunk = max(self.max_chunk, chunk)
                v = s.get('validity')
                if v == 'helpful':
                    self.helpful[key] += 1
                    if s.get('rfrom'):
                        self.sources[key].add(s['rfrom'])
                elif v == 'unnecessary':
                    self.unnecessary[key] += 1
                else:
                    self.redundant[key] += 1
            elif name in RECODE_NAMES:
                self.recode_spans[s['node']] += 1
                self.recode_gens[s['node']].add((s['trace'], chunk))
                self.recode_chunk_ids[s['node']].add(chunk)

    @property
    def chunks_per_message(self):
        return self.max_chunk + 1 if self.helpful or self.completed else 0

    def receiver_keys(self):
        """Every (trace, node, chunk) a non-publishing node has any span for."""
        keys = set(self.completed) | set(self.helpful) | set(self.redundant) | set(self.unnecessary)
        return {k for k in keys if k[1] not in self.pub_nodes}


# --- sections --------------------------------------------------------------


def section_config(gws, out):
    print("\n[1] applied config (read back from self_info, not assumed)")
    print(f"  {'gateway':<10} {'cluster':<16} {'chain':<6} {'k':>3} {'p':>5} {'f':>5} {'shard':>6} {'prop':>5}")
    broken = []
    for gw in gws:
        r = gw.rlnc
        print(f"  {gw.name:<10} {gw.info.get('gateway_cluster_id',''):<16} {gw.info.get('chain',''):<6} "
              f"{int(r.get('rlnc_shard_factor', 0)):>3} {r.get('publisher_shard_multiplier', 0):>5} "
              f"{round(float(r.get('forward_shard_threshold', 0)), 4):>5} "
              f"{int(r.get('max_shard_size', 0)):>6} "
              f"{str(gw.info.get('propagation_enabled')):>5}")
    k = int(gws[0].rlnc.get("rlnc_shard_factor", 0))
    # Served as a float32, so it reads back as 0.8500000238418579. Round it
    # before it reaches a table or a JSON summary.
    f = round(float(gws[0].rlnc.get("forward_shard_threshold", 0)), 4)
    p = round(float(gws[0].rlnc.get("publisher_shard_multiplier", 0)), 4)
    for gw in gws:
        r = gw.rlnc
        if gw.info.get("gateway_cluster_id") != EXPECTED_CLUSTER_ID:
            broken.append(f"{gw.name}: cluster_id {gw.info.get('gateway_cluster_id')!r}; "
                          f"a mismatch presents as a mesh with no edges, never as an auth error")
        if gw.info.get("chain") != EXPECTED_CHAIN:
            broken.append(f"{gw.name}: chain {gw.info.get('chain')!r}, expected {EXPECTED_CHAIN!r}")
        if int(r.get("rlnc_shard_factor", 0)) != k:
            broken.append(f"{gw.name}: k={r.get('rlnc_shard_factor')} differs from gateway1's {k}; "
                          f"the config proxy did not reach every node")
        if int(r.get("max_shard_size", 0)) != SHARD_BUDGET - k:
            broken.append(f"{gw.name}: derived max_shard_size {r.get('max_shard_size')} != "
                          f"{SHARD_BUDGET - k}; a config fetch failure is silent and leaves k at the "
                          f"built-in 4, so check the config proxy before reading anything below")
        if not gw.info.get("propagation_enabled"):
            broken.append(f"{gw.name}: propagation_enabled is false")
    gate = int(k * f)
    print(f"  k={k} p={p} f={f}: a node forwards recoded symbols only once its rank exceeds "
          f"int(k*f)={gate}, i.e. from rank {gate + 1} of {k}")
    out["k"], out["f"], out["p"], out["forward_gate"] = k, f, p, gate
    return broken


def section_auth(gws, out):
    print("\n[2] authentication and datagram sessions")
    print(f"  {'gateway':<10} {'peers':>5} {'authorized':>11} {'rejected':>9} {'paths':>12}")
    broken = []
    for gw in gws:
        d = gw.datagram
        print(f"  {gw.name:<10} {gw.peers:>5} {int(gw.handshakes_ok):>11} {int(gw.handshakes_rejected):>9} "
              f"{str(d.get('paths_confirmed')) + '/' + str(d.get('peers_total')):>12}")
    for gw in gws:
        d = gw.datagram
        if gw.handshakes_rejected > 0:
            broken.append(f"{gw.name}: {int(gw.handshakes_rejected)} handshakes rejected on cluster binding")
        if not d.get("enabled"):
            broken.append(f"{gw.name}: the datagram data plane is disabled")
        if d.get("peers_total", 0) == 0:
            broken.append(f"{gw.name}: no peers at all; this node never joined")
        elif d.get("paths_confirmed") != d.get("peers_total"):
            broken.append(f"{gw.name}: paths_confirmed={d.get('paths_confirmed')} != "
                          f"peers_total={d.get('peers_total')}; an unconfirmed peer takes the stream "
                          f"fallback silently")
    total_ok = sum(gw.handshakes_ok for gw in gws)
    print(f"  fleet authorized={int(total_ok)} rejected={int(sum(gw.handshakes_rejected for gw in gws))}")
    out["handshakes_authorized"] = int(total_ok)
    return broken


def section_topology(gws, out):
    print("\n[3] topology: peers per gateway (P)")
    peers = [gw.peers for gw in gws]
    print("  " + "  ".join(f"{gw.name.replace('gateway','gw')}={gw.peers}" for gw in gws))
    print(f"  P: min={min(peers)} p50={pct(peers,50)} max={max(peers)} mean={sum(peers)/len(peers):.1f}")
    print("  Note: the bootstrap hands out at most 7 peers, once, at startup, so the initial graph "
          "is not a clique above 8 gateways; DHT discovery then fills it in, and P is what settled.")
    out["peers_min"], out["peers_p50"], out["peers_max"] = min(peers), pct(peers, 50), max(peers)
    out["peers_mean"] = sum(peers) / len(peers)
    return []


def section_transport(gws, out):
    print("\n[4] transport: which path carried the symbols, and who sent at all")
    print(f"  {'gateway':<10} {'hook':>8} {'fallback':>9} {'fwd_drops':>10} {'ing_drops':>10}")
    broken = []
    for gw in gws:
        print(f"  {gw.name:<10} {int(gw.hook_sends):>8} {int(gw.fallback_sends):>9} "
              f"{int(gw.forward_drops):>10} {int(gw.ingress_drops):>10}")
    senders = [gw for gw in gws if gw.hook_sends > 0]
    print(f"  fleet hook={int(sum(gw.hook_sends for gw in gws))} "
          f"fallback={int(sum(gw.fallback_sends for gw in gws))}")
    print(f"  gateways that ever sent a datagram: {len(senders)} of {len(gws)} "
          f"({', '.join(g.name for g in senders) if senders else 'none'})")
    for gw in gws:
        if gw.fallback_sends > 0:
            broken.append(f"{gw.name}: {int(gw.fallback_sends)} sends took the stream fallback, so the "
                          f"datagram path did not carry all the traffic")
    out["senders"] = len(senders)
    out["sender_names"] = [g.name for g in senders]
    return broken


def section_delivery(gws, expect_messages, out):
    print("\n[5] delivery")
    broken = []
    publishers = [gw for gw in gws if gw.published > 0]
    if not publishers:
        broken.append("no gateway published anything to the mesh; the publisher never reached the CL "
                      "ingress (the gateway subscribes to beacon_block only after an eth2 status handshake)")
        out["delivery_fraction"] = None
        return broken
    if len(publishers) > 1:
        broken.append(f"{len(publishers)} gateways published; exactly one publishing ingress is expected")
    pub = publishers[0]
    published = int(pub.published)
    subs = [gw for gw in gws if gw is not pub]
    print(f"  publisher {pub.name} published {published} beacon_block messages to the mesh")
    got_total = 0
    for gw in subs:
        got = int(gw.delivered)
        got_total += got
        rate = (100.0 * got / published) if published else 0.0
        print(f"  {gw.name:<10} decoded={got:>4}/{published:<4} ({rate:5.1f}%)")
    frac = (got_total / (published * len(subs))) if published and subs else 0.0
    print(f"  FLEET DELIVERY: {got_total}/{published * len(subs)} subscriber-messages = {100.0 * frac:.1f}%")
    if expect_messages is not None and published != expect_messages:
        broken.append(f"{pub.name} published {published} messages, expected {expect_messages}; blocks "
                      f"were dropped at the CL ingress before they reached the mesh")
    out.update({
        "publisher": pub.name,
        "published": published,
        "subscribers": len(subs),
        "delivered_total": got_total,
        "delivery_fraction": frac,
        "per_subscriber": {gw.name: int(gw.delivered) for gw in subs},
    })
    return broken


def section_rank(view, id_to_name, expect_receivers, out):
    """Rank reached per receiver-chunk: the diagnostic the whole tier exists for."""
    k = out.get("k") or 0
    gate = out.get("forward_gate") or 0
    print(f"\n[6] rank reached per receiver-chunk (need k={k}, forwards above {gate})")
    keys = view.receiver_keys()
    if not keys:
        print("  no receiver spans at all: either nothing was published or the collector saw no traffic")
        return []
    chunks = view.chunks_per_message
    ranks = [view.helpful[key] for key in keys]
    expected_total = len(view.pub_traces) * chunks * expect_receivers
    silent = max(0, expected_total - len(keys))
    print(f"  generations published: {len(view.pub_traces)}   chunks per message: {chunks}   "
          f"receivers: {expect_receivers}")
    print(f"  receiver-chunks with any span: {len(keys)} of {expected_total} expected"
          + (f"   ({silent} saw no symbol at all, rank 0 by definition)" if silent else ""))
    ranks_with_silent = ranks + [0] * silent
    reached = sum(1 for r in ranks_with_silent if r >= k)
    print(f"  RANK: min={min(ranks_with_silent)} p10={pct(ranks_with_silent,10)} "
          f"p50={pct(ranks_with_silent,50)} p90={pct(ranks_with_silent,90)} max={max(ranks_with_silent)}")
    print(f"  reached full rank {k}: {reached} of {len(ranks_with_silent)} receiver-chunks "
          f"({100.0 * reached / len(ranks_with_silent):.1f}%)")
    above = sum(1 for r in ranks_with_silent if r > gate)
    print(f"  crossed the forwarding gate (rank > {gate}): {above} of {len(ranks_with_silent)} "
          f"({100.0 * above / len(ranks_with_silent):.1f}%)")
    hist = defaultdict(int)
    for r in ranks_with_silent:
        hist[r] += 1
    print("  histogram (rank: receiver-chunks): "
          + " ".join(f"{r}:{hist[r]}" for r in sorted(hist)))
    # The stalled chunks on their own. Pooling them with the ones that decoded
    # hides the plateau: a fleet where one generation in four dies still shows a
    # median rank of k, because three quarters of the population is at k.
    stalled = [r for r in ranks_with_silent if r < k]
    if stalled:
        print(f"  STALLED receiver-chunks (rank < {k}): {len(stalled)}   "
              f"rank min={min(stalled)} p50={pct(stalled,50)} max={max(stalled)}")
        out.update({"stalled_chunks": len(stalled), "stalled_rank_min": min(stalled),
                    "stalled_rank_p50": pct(stalled, 50), "stalled_rank_max": max(stalled)})
    # Per generation, because the gate fails for the whole fleet at once: a
    # generation whose cascade never starts is lost by every node simultaneously,
    # which is a different shape from a few nodes falling behind.
    per_gen = defaultdict(lambda: [0, 0])
    for key in keys:
        cell = per_gen[key[0]]
        cell[0] += 1
        if view.helpful[key] >= k:
            cell[1] += 1
    if per_gen:
        shape = " ".join(f"{done}/{seen}" for _, (seen, done) in sorted(per_gen.items()))
        print(f"  per generation, receiver-chunks that reached full rank: {shape}")
    per_node = defaultdict(list)
    for key in keys:
        per_node[key[1]].append(view.helpful[key])
    print("  per receiver: rank p50/max over its chunks")
    for node, rs in sorted(per_node.items(), key=lambda kv: id_to_name.get(kv[0], kv[0])):
        print(f"    {id_to_name.get(node, node[:16]):<10} chunks={len(rs):>4} p50={pct(rs,50):>3} "
              f"max={max(rs):>3} reached_k={sum(1 for r in rs if r >= k)}")
    srcs = [len(view.sources[key]) for key in keys if view.sources[key]]
    if srcs:
        print(f"  distinct upstream sources of helpful symbols: p50={pct(srcs,50)} max={max(srcs)}")
    out.update({
        "rank_min": min(ranks_with_silent),
        "rank_p50": pct(ranks_with_silent, 50),
        "rank_p90": pct(ranks_with_silent, 90),
        "rank_max": max(ranks_with_silent),
        "rank_reached_k_fraction": reached / len(ranks_with_silent),
        "rank_above_gate_fraction": above / len(ranks_with_silent),
        "receiver_chunks": len(ranks_with_silent),
        "chunks_per_message": chunks,
    })
    return []


def section_recode(view, expect_receivers, id_to_name, out):
    print("\n[7] rlnc.symbol.recode spans (a node recodes only after crossing the gate)")
    receivers = {n for n in set(view.recode_spans) | {k[1] for k in view.receiver_keys()}
                 if n not in view.pub_nodes}
    recoders = [n for n in receivers if view.recode_spans.get(n, 0) > 0]
    total = sum(view.recode_spans[n] for n in recoders)
    print(f"  receivers that recoded at all: {len(recoders)} of {expect_receivers}   total spans: {total}")
    for node in sorted(receivers, key=lambda n: id_to_name.get(n, n)):
        n_spans = view.recode_spans.get(node, 0)
        gens = len(view.recode_gens.get(node, ()))
        ids = sorted(view.recode_chunk_ids.get(node, ()))
        print(f"    {id_to_name.get(node, node[:16]):<10} spans={n_spans:>6} generations={gens:>4} "
              f"chunk_ids={ids if ids else '-'}")
    out["recoders"] = len(recoders)
    out["recode_spans_total"] = total
    return []


def section_symbols(view, out):
    print("\n[8] symbol composition on receivers")
    keys = view.receiver_keys()
    h = sum(view.helpful[key] for key in keys)
    u = sum(view.redundant[key] for key in keys)
    n = sum(view.unnecessary[key] for key in keys)
    tot = h + u + n
    if not tot:
        print("  no symbols received")
        return []
    print(f"  helpful={h} ({100.0*h/tot:.1f}%)  redundant/inconsistent={u} ({100.0*u/tot:.1f}%)  "
          f"unnecessary={n} ({100.0*n/tot:.1f}%)  total={tot}")
    print("  A starved fleet shows almost no unnecessary symbols: nothing arrives after a decode "
          "because there was no decode. A healthy small fleet is mostly unnecessary.")
    out.update({"symbols_helpful": h, "symbols_redundant": u, "symbols_unnecessary": n})
    return []


def verdict(out):
    print("\n" + "=" * 78)
    frac = out.get("delivery_fraction")
    k, gate = out.get("k"), out.get("forward_gate")
    if frac is None:
        print(f"VERDICT from spans alone: rank p50={out.get('rank_p50')} max={out.get('rank_max')} "
              f"of {k}, receiver-chunks at full rank "
              f"{100.0 * out.get('rank_reached_k_fraction', 0):.1f}%")
        return
    reached = out.get("rank_reached_k_fraction", 0.0)
    above = out.get("rank_above_gate_fraction", 0.0)
    print(f"VERDICT at P: min={out.get('peers_min')} p50={out.get('peers_p50')} "
          f"max={out.get('peers_max')}, k={k}, f={out.get('f')} (gate at rank >{gate})")
    print(f"  delivery {100.0*frac:.1f}%   rank p50={out.get('rank_p50')} max={out.get('rank_max')} "
          f"of {k}   receiver-chunks at full rank {100.0*reached:.1f}%   "
          f"past the gate {100.0*above:.1f}%")
    print(f"  gateways that ever sent a datagram: {out.get('senders')}, "
          f"receivers that recoded: {out.get('recoders')}")
    if frac >= 0.999:
        print("  FULL DELIVERY: this peer count does not reproduce the failure. The direct share plus "
              "the one-hop relay of the publisher's systematic symbols still carries enough nodes past "
              "the gate. Raise --gateways or --forward-threshold and sweep again.")
    elif frac <= 0.001:
        print("  REPRODUCED: nothing decoded. Rank plateaus below k and the forwarding gate never "
              "opens, so no node has anything to forward.")
    else:
        print("  PARTIAL: some nodes cross the gate and some do not. This is the knee of the curve; "
              "the delivery-versus-P and delivery-versus-f sweeps around this point are the signal.")
    print("=" * 78)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--gateways", type=int, default=12)
    ap.add_argument("--base-port", type=int, default=48151, help="host port of gateway1; the rest follow")
    ap.add_argument("--expect-messages", type=int, default=None)
    ap.add_argument("--spans", default=None, help="path to the captured otel-collector log")
    ap.add_argument("--node-map", dest="node_map", default=None,
                    help="JSON {mump2p node id: gateway name}, as run.sh reads it out of the logs")
    ap.add_argument("--offline", action="store_true",
                    help="do not scrape; re-analyse an archived run from its spans alone")
    ap.add_argument("--served-config", dest="served_config", default=None,
                    help="the run's served-config.json, for k/p/f when the fleet is gone")
    ap.add_argument("--json", dest="json_path", default=None, help="also write the summary as JSON")
    args = ap.parse_args()

    gws = [Gateway(f"gateway{i}", args.base_port + i - 1) for i in range(1, args.gateways + 1)]
    print(f"mid-scale dissemination report: {args.gateways} gateways on ports "
          f"{args.base_port}..{args.base_port + args.gateways - 1}")

    out = {"gateways": args.gateways}
    live = not args.offline
    try:
        if live:
            for gw in gws:
                gw.scrape()
    except Broken as err:
        # Not fatal: the span log plus the served config is enough to redo the
        # rank analysis of a run whose containers are long gone.
        print(f"\nfleet not reachable ({err})")
        live = False
    if not live:
        print("re-analysing from the artifacts alone; sections 1-5 need a live fleet")

    broken = []
    if live:
        broken += section_config(gws, out)
        broken += section_auth(gws, out)
        broken += section_topology(gws, out)
        broken += section_transport(gws, out)
        broken += section_delivery(gws, args.expect_messages, out)
    elif args.served_config:
        try:
            with open(args.served_config, encoding="utf-8") as fh:
                cfg = json.load(fh)
            out["k"] = int(cfg.get("rlnc_shard_factor", 0))
            out["f"] = round(float(cfg.get("forward_shard_threshold", 0)), 4)
            out["p"] = round(float(cfg.get("publisher_shard_multiplier", 0)), 4)
            out["forward_gate"] = int(out["k"] * out["f"])
            print(f"served config: k={out['k']} p={out['p']} f={out['f']} "
                  f"(gate at rank >{out['forward_gate']})")
        except (OSError, json.JSONDecodeError, ValueError) as err:
            print(f"could not read {args.served_config}: {err}")

    if args.spans:
        try:
            spans = parse_spans(args.spans)
        except OSError as err:
            print(f"\ncould not read {args.spans}: {err}")
            spans = []
        if spans:
            view = SpanView(spans)
            # The span resource carries the mump2p node id, which is that node's
            # own peer ID: it is in neither self_info nor /metrics, so run.sh
            # reads it out of the startup log and passes the mapping in here.
            id_to_name = {}
            for gw in gws:
                for ident in gw.node_ids:
                    id_to_name[ident] = gw.name
            if args.node_map:
                try:
                    with open(args.node_map, encoding="utf-8") as fh:
                        id_to_name.update(json.load(fh))
                except (OSError, json.JSONDecodeError) as err:
                    print(f"  (no node-name mapping: {err}; nodes are shown by peer ID)")
            print(f"\nspans parsed: {len(spans)}   publishing nodes: "
                  f"{[id_to_name.get(n, n[:16]) for n in sorted(view.pub_nodes)]}")
            receivers = args.gateways - max(1, len(view.pub_nodes))
            broken += section_rank(view, id_to_name, receivers, out)
            broken += section_recode(view, receivers, id_to_name, out)
            broken += section_symbols(view, out)
        else:
            print("\nno spans parsed: the collector log is empty or was not captured")
    else:
        print("\n[6-8] skipped: no --spans given, so there is no rank diagnostic")

    verdict(out)

    if broken:
        print(f"\nPRECONDITIONS NOT MET ({len(broken)}); the numbers above are not trustworthy:")
        for reason in broken:
            print(f"  - {reason}")
    elif live:
        print("\nPreconditions held: config applied, peers authenticated, every path confirmed, "
              "nothing on the stream fallback. The delivery and rank numbers are real.")
    else:
        print("\nPreconditions were not re-checked: this is an offline re-analysis. Whether they "
              "held is in the run's own report.txt.")

    out["preconditions_ok"] = not broken
    out["precondition_failures"] = broken
    if args.json_path:
        with open(args.json_path, "w", encoding="utf-8") as fh:
            json.dump(out, fh, indent=2, sort_keys=True)
            fh.write("\n")

    return 1 if broken else 0


if __name__ == "__main__":
    sys.exit(main())
