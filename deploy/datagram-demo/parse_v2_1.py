#!/usr/bin/env python3
# Parser for the OTel collector's debug-exporter log: turns the RLNC spans a run
# produced back into per-generation numbers.
#
# Spans are grouped by (trace, node, chunk_id). A trace is one published
# generation, a node is one gateway (keyed on the mump2p.node_id resource
# attribute) and a chunk is one RLNC generation within a message, so a message
# sharded into several chunks yields one rlnc.decode span per chunk per node
# rather than one per message. A log with no rlnc.chunk_id attribute defaults to
# chunk 0, which collapses every per-chunk computation back to a per-node one.
#
# Two span-name spellings are matched for receive and recode
# ("rlnc.shard.*" and "rlnc.symbol.*"), so a log from either naming parses.
#
#   usage: parse_v2_1.py <otel-collector.log> [--expect-chunks N]
#
# --expect-chunks is advisory: it prints whether the observed chunk count is the
# intended one and never changes the exit code.
#
# End-to-end latency is computed purely from spans (publish span start -> the end
# of that node's last chunk decode). All the nodes share one clock here, so the
# cross-node delta is exact and does not depend on message timestamps.
import sys, re
from collections import defaultdict
from datetime import datetime, timezone

LINE = re.compile(r'stderr F (.*)$')
TS = re.compile(r'(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:\.\d+)?) \+0000 UTC')
RECV_PREFIXES = ('rlnc.shard.recv.', 'rlnc.symbol.recv.')
RECODE_NAMES = ('rlnc.shard.recode', 'rlnc.symbol.recode')

def parse_ns(s):
    m = TS.search(s)
    if not m: return None
    t = m.group(1)
    if '.' in t:
        base, frac = t.split('.')
        frac = (frac + '000000000')[:9]
    else:
        base, frac = t, '000000000'
    dt = datetime.strptime(base, '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
    return int(dt.timestamp()) * 1_000_000_000 + int(frac)

def content(raw):
    m = LINE.search(raw)
    return m.group(1) if m else raw

# manual argv parsing (no argparse) to preserve the existing "parse_v2_1.py <logfile>" positional
# interface exactly -- flags are optional and may appear anywhere after the logfile.
argv = sys.argv[1:]
expect_chunks = None
positional = []
i = 0
while i < len(argv):
    a = argv[i]
    if a == '--expect-chunks':
        i += 1; expect_chunks = int(argv[i])
    else:
        positional.append(a)
    i += 1
logfile = positional[0]

spans = []            # each: dict(name,trace,node,start,end,validity,rfrom,shard,completed,chunk,recode_inputs)
cur_node = None
cur = None            # current span being built

def flush():
    global cur
    if cur and cur.get('name'):
        cur['node'] = cur_node
        spans.append(cur)
    cur = None

for raw in open(logfile, errors='replace'):
    c = content(raw).rstrip('\n')
    s = c.strip()
    if s.startswith('Resource attributes:'):
        flush(); continue
    if s.startswith('-> mump2p.node_id:'):
        m = re.search(r'Str\((.*)\)', s)
        if m: cur_node = m.group(1)
        continue
    if s.startswith('-> service.instance.id:') and cur_node is None:
        m = re.search(r'Str\((.*)\)', s)
        if m: cur_node = m.group(1)
        continue
    if s.startswith('Span #'):
        flush(); cur = {}; continue
    if cur is None:
        continue
    if s.startswith('Trace ID'):
        m = re.search(r':\s*([0-9a-f]+)', s); cur['trace'] = m.group(1) if m else None
    elif s.startswith('Name'):
        cur['name'] = s.split(':',1)[1].strip()
    elif s.startswith('Start time'):
        cur['start'] = parse_ns(s)
    elif s.startswith('End time'):
        cur['end'] = parse_ns(s)
    elif s.startswith('-> rlnc.symbol.validity:'):
        m = re.search(r'Str\((.*)\)', s); cur['validity'] = m.group(1) if m else None
    elif s.startswith('-> rlnc.received_from:'):
        m = re.search(r'Str\((.*)\)', s); cur['rfrom'] = m.group(1) if m else None
    elif s.startswith('-> rlnc.shard.id:') or s.startswith('-> rlnc.symbol.id:'):
        m = re.search(r'Str\((.*)\)', s); cur['shard'] = m.group(1) if m else None
    elif s.startswith('-> rlnc.decode.completed:'):
        cur['completed'] = 'true' in s.lower()
    elif s.startswith('-> rlnc.recode.input.count:'):
        m = re.search(r'Int\((\d+)\)', s); cur['recode_inputs'] = int(m.group(1)) if m else None
    elif s.startswith('-> rlnc.chunk_id:'):
        m = re.search(r'Int\((\d+)\)', s); cur['chunk'] = int(m.group(1)) if m else 0
flush()

spans = [s for s in spans if s.get('name') and s.get('trace')]

# publish time per trace (min publish-span start); publisher node(s)
pub_t = {}; pubnodes = set()
for s in spans:
    if s['name'] == 'rlnc.publish' and s.get('start'):
        pub_t[s['trace']] = min(pub_t.get(s['trace'], s['start']), s['start'])
        pubnodes.add(s['node'])

# decode/recv are keyed by (trace, node, chunk_id); chunk_id defaults to 0 when the attribute is
# absent, so a single-chunk generation reduces to one entry per node.
decode = {}                       # (trace,node,chunk) -> best decode span for that chunk
recv = defaultdict(list)          # (trace,node,chunk) -> [(start,validity,rfrom)]
recode_ct = defaultdict(int)      # (trace,node) -> recode span count (unchanged granularity)
for s in spans:
    chunk = s.get('chunk', 0) or 0
    key = (s['trace'], s['node'], chunk)
    if s['name'] == 'rlnc.decode':
        if key not in decode or (s.get('completed') and not decode[key].get('completed')):
            decode[key] = s
    elif s['name'].startswith(RECV_PREFIXES) and s.get('start'):
        recv[key].append((s['start'], s.get('validity'), s.get('rfrom')))
    elif s['name'] in RECODE_NAMES:
        recode_ct[(s['trace'], s['node'])] += 1

def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs)-1, int(round(p/100*(len(xs)-1))))] if xs else 0

# group decode/recv chunk-keys by (trace,node) so message-level delivery ("all chunks reached
# rank k / decode.completed") and end-to-end latency stay message-level as before, while
# distinct/first80/tail20/complexity/composition become chunk-aware.
chunks_by_node = defaultdict(set)
for (trace, node, chunk) in decode:
    chunks_by_node[(trace, node)].add(chunk)

# hoisted so this is available even when nothing delivered (chunks_by_node is populated by decode
# spans on receivers regardless of completion, unlike the old in-loop update which only ran for
# nodes that reached the "delivered" continue-past-checks below).
max_chunk_seen = max((max(cids)+1 for (t,n),cids in chunks_by_node.items() if n not in pubnodes), default=0)
n_partial_chunkset = 0   # receivers that saw only SOME chunks; not deliveries, counted for visibility

lat=[]; distinct=[]; first80=[]; tail20=[]; starved=0; flooded=0; ndeliv=0
recodes_per_block=[]
complexity=[]                                   # total symbols recv'd per delivered node (dim 2)
comp_helpful=[]; comp_unhelpful=[]; comp_unnecessary=[]     # per-chunk-decode medians (dim 3)
comp_tot_h=comp_tot_u=comp_tot_n=0

for (trace, node), cids in chunks_by_node.items():
    if node in pubnodes: continue          # analyze receivers only

    # message delivered <=> every chunk's decode span completed (handleDeliver force-closes any
    # still-open chunk span as completed=true when the whole message is delivered, so this is
    # equivalent to "all chunks reached rank k").
    # `cids` is the OBSERVED chunk set for this node, so requiring the full set matters: a node that
    # saw only 1 of 4 chunks would otherwise pass this test and inflate span-derived delivery. A
    # message is not delivered until every one of its chunks is.
    if max_chunk_seen and len(cids) != max_chunk_seen:
        n_partial_chunkset += 1
        continue
    dspans = [decode[(trace, node, c)] for c in cids]
    if not all(d.get('completed') for d in dspans): continue
    total_helps = sum(1 for c in cids for e in recv.get((trace, node, c), []) if e[1] == 'helpful')
    if total_helps == 0: continue
    ndeliv += 1

    # end-to-end latency stays message-level: the message is delivered once its LAST chunk ends.
    ends = [d['end'] for d in dspans if d.get('end')]
    if ends and trace in pub_t:
        lat.append((max(ends) - pub_t[trace]) / 1e6)

    total_symbols_node = 0
    for c in sorted(cids):
        ev = sorted(recv.get((trace, node, c), []))
        total_symbols_node += len(ev)
        helps = [e for e in ev if e[1] == 'helpful']

        # dim 3: symbol composition, tallied per chunk-decode instance.
        h = len(helps)
        u = sum(1 for e in ev if e[1] in ('redundant', 'inconsistent'))
        n = sum(1 for e in ev if e[1] == 'unnecessary')
        comp_helpful.append(h); comp_unhelpful.append(u); comp_unnecessary.append(n)
        comp_tot_h += h; comp_tot_u += u; comp_tot_n += n

        if not helps: continue
        distinct.append(len({e[2] for e in helps if e[2]}))
        k = len(helps); ci = min(k-1, int(k*0.8))
        t0, t80, tend = helps[0][0], helps[ci][0], helps[-1][0]
        first80.append((t80-t0)/1e6); tail20.append((tend-t80)/1e6)
        if (tend - t80) > 20e6:
            gap = [e for e in ev if t80 < e[0] <= tend and e[1] != 'helpful']
            if gap: flooded += 1
            else: starved += 1

    complexity.append(total_symbols_node)      # dim 2: total symbols per delivered (trace,node)

for (trace,node),ct in recode_ct.items():
    if node in pubnodes: continue
    recodes_per_block.append(ct)

# publisher-to-arrival deltas: there are no per-hop send spans (see file header), so this is the
# only latency signal available -- everything below is publisher-to-receiver, not a true one-way hop.
n_pub_recv_events = 0
skew_mags_ms = []      # magnitude of recv_start < pub_t[trace] events (physically impossible -> clock skew)
for (trace, node, chunk), events in recv.items():
    if trace not in pub_t: continue
    for (start, validity, rfrom) in events:
        n_pub_recv_events += 1
        if start < pub_t[trace]:
            skew_mags_ms.append((pub_t[trace] - start) / 1e6)

n_traces = len(pub_t)
print(f"traces (published generations): {n_traces}")
print(f"publisher node(s): {len(pubnodes)}  {sorted(pubnodes)[:3]}")
chunks_per_msg = max_chunk_seen if max_chunk_seen else 1
print(f"chunks per message: {chunks_per_msg}")
if expect_chunks is not None:
    status = "OK" if chunks_per_msg == expect_chunks else "MISMATCH"
    print(f"CHUNK ASSERT: observed={chunks_per_msg} expected={expect_chunks} {status}")
print(f"delivered receiver-generations analyzed: {ndeliv}")
if n_partial_chunkset:
    print(f"PARTIAL DELIVERY: {n_partial_chunkset} receiver-generations saw only some of {max_chunk_seen} chunks (excluded from the delivered count)")
if lat:
    print(f"END-TO-END latency (deliver - publish): p50={pct(lat,50):.0f}ms p95={pct(lat,95):.0f}ms p99={pct(lat,99):.0f}ms max={max(lat):.0f}ms")
if distinct:
    khelp = pct([len([e for e in sorted(recv.get(key,[])) if e[1]=='helpful'])
                 for key in decode if decode[key].get('completed') and key[1] not in pubnodes], 50)
    print(f"helpful symbols per chunk (rank): p50={khelp}")
    print(f"distinct upstream sources of helpful symbols: p50={pct(distinct,50)} p90={pct(distinct,90)} max={max(distinct)} min={min(distinct)}")
    print(f"time to gather first 80% of rank: p50={pct(first80,50):.0f}ms p90={pct(first80,90):.0f}ms")
    print(f"time to gather LAST 20% of rank:  p50={pct(tail20,50):.0f}ms p90={pct(tail20,90):.0f}ms p95={pct(tail20,95):.0f}ms max={max(tail20):.0f}ms")
    print(f"nodes with >20ms completion tail: {starved+flooded} (starved={starved}, flooded-with-redundant={flooded})")
if recodes_per_block:
    print(f"recode spans per receiver-generation: p50={pct(recodes_per_block,50)} p90={pct(recodes_per_block,90)} total={sum(recodes_per_block)}")
if complexity:
    # dim 2: Symbol Complexity -- total symbols (helpful+redundant+inconsistent+unnecessary) received
    # per delivered (trace,node), summed across all its chunks.
    print(f"SYMBOL COMPLEXITY: total symbols per delivered node: p50={pct(complexity,50)} p90={pct(complexity,90)}")
if comp_helpful:
    tot = comp_tot_h + comp_tot_u + comp_tot_n
    pf = lambda x: (100.0*x/tot) if tot else 0.0
    # dim 3: Symbol Composition -- per-chunk-decode medians, plus the pooled fraction of total symbols.
    print(f"SYMBOL COMPOSITION: median per chunk-decode: helpful={pct(comp_helpful,50)} unhelpful={pct(comp_unhelpful,50)} unnecessary={pct(comp_unnecessary,50)}")
    print(f"SYMBOL COMPOSITION: fraction of total: helpful={pf(comp_tot_h):.1f}% unhelpful={pf(comp_tot_u):.1f}% unnecessary={pf(comp_tot_n):.1f}%")
if n_pub_recv_events:
    print(f"CLOCK SKEW: {len(skew_mags_ms)} receive-before-publish events")
    if skew_mags_ms:
        print(f"CLOCK SKEW: max magnitude={max(skew_mags_ms):.1f}ms")
print(f"total spans parsed: {len(spans)}")
