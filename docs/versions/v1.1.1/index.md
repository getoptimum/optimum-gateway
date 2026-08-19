# Optimum Gateway - User Guide

> **Recommended upgrade:** v1.1.1 is the current release. Partners on v1.0.2 should upgrade when convenient - networking and CL peering are unchanged.

> **Release Notes:** [What's new in v1.1.1](./release_notes.md)

> **Security audit:** ProbeLab, 2026 - [Full report](https://cdn.probelab.io/media/documents/2026-08-ProbeLab-Security_Audit_Report_Optimum_Gateway.pdf)

The **Optimum Gateway** bridges your **Ethereum Consensus Layer (CL) client** with the **mump2p** network.

## What does Optimum Gateway do?

* **Problem**: Validators rely on CL gossip (libp2p) for block and attestation propagation. Latency variance hurts performance.
* **Gateway Role**: Bridges your local CL client to the **mump2p** network for both blocks and attestations.
* **Result**: Faster block and attestation propagation, reduced latency, improved validator rewards.

## Architecture

<svg viewBox="0 0 1280 760" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Optimum Gateway architecture" style="width:100%;height:auto;max-width:1100px;display:block;margin:1.5rem auto;">
  <defs>
    <marker id="op-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse" markerUnits="userSpaceOnUse">
      <path d="M0,0 L10,5 L0,10 L2.2,5 Z" fill="currentColor" fill-opacity="0.55"></path>
    </marker>
  </defs>
  <line x1="300" y1="220" x2="436" y2="220" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#op-arrow)"></line>
  <text x="370" y="208" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">subscribe topic</text>
  <line x1="840" y1="220" x2="976" y2="220" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#op-arrow)"></line>
  <text x="910" y="208" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">publish</text>
  <line x1="500" y1="396" x2="500" y2="484" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-start="url(#op-arrow)" marker-end="url(#op-arrow)"></line>
  <text x="486" y="418" text-anchor="end" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">↑  peer list · fork digest · known validators</text>
  <text x="486" y="442" text-anchor="end" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">↓  heartbeat</text>
  <line x1="740" y1="396" x2="740" y2="569" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#op-arrow)"></line>
  <text x="754" y="418" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">↓  metrics + logs · remote push</text>
  <path d="M 80,120 L 280,120 A 20,20 0 0 1 300,140 L 300,300 A 20,20 0 0 1 280,320 L 80,320 A 20,20 0 0 1 60,300 L 60,140 A 20,20 0 0 1 80,120 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="84" y="154" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Client</text>
  <text x="84" y="192" text-anchor="start" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">CL client</text>
  <text x="84" y="232" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">beacon_block, attestations</text>
  <text x="84" y="254" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.55">64 subnets</text>
  <path d="M 520,70 L 820,70 A 20,20 0 0 1 840,90 L 840,310 A 80,80 0 0 1 760,390 L 460,390 A 20,20 0 0 1 440,370 L 440,150 A 80,80 0 0 1 520,70 Z" fill="#B87CFF" fill-opacity="0.07" stroke="#B87CFF" stroke-opacity="1" stroke-width="2"></path>
  <text x="476" y="112" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="#B87CFF" fill-opacity="1" style="text-transform:uppercase">Gateway</text>
  <text x="476" y="158" text-anchor="start" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="28" font-weight="400" letter-spacing="-0.5" fill="currentColor">Optimum Gateway</text>
  <circle cx="484" cy="208" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="500" y="212" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Subscribes to CL topics</text>
  <circle cx="484" cy="240" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="500" y="244" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Aggregates &amp; packs attestations</text>
  <circle cx="484" cy="272" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="500" y="276" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Publishes to mump2p</text>
  <circle cx="484" cy="304" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="500" y="308" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Pushes remote telemetry</text>
  <path d="M 1000,120 L 1200,120 A 20,20 0 0 1 1220,140 L 1220,300 A 20,20 0 0 1 1200,320 L 1000,320 A 20,20 0 0 1 980,300 L 980,140 A 20,20 0 0 1 1000,120 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="1004" y="154" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Network</text>
  <text x="1004" y="192" text-anchor="start" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Optimum Network</text>
  <text x="1004" y="232" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">Interconnected</text>
  <text x="1004" y="254" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">mump2p nodes</text>
  <path d="M 395,510 A 105,20 0 0 1 605,510 L 605,690 A 105,20 0 0 1 395,690 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <path d="M 395,510 A 105,20 0 0 0 605,510" fill="none" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="500" y="582" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Discovery</text>
  <text x="500" y="612" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Bootstrap</text>
  <text x="500" y="638" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">Stateless discovery</text>
  <path d="M 642,575 L 838,575 A 22,45 0 0 1 838,665 L 642,665 A 22,45 0 0 1 642,575 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <path d="M 838,575 A 22,45 0 0 0 838,665" fill="none" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="712" y="608" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Telemetry</text>
  <text x="712" y="634" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="20" font-weight="400" letter-spacing="-0.5" fill="currentColor">Mimir · Loki</text>
  <text x="712" y="656" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="12" font-weight="500" fill="currentColor" fill-opacity="0.6">via Cloudflare</text>
  <path d="M 230,510 L 230,486 Q 230,474 242,474 L 528,474 Q 540,474 540,462 L 540,392" fill="none" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#op-arrow)"></path>
  <text x="552" y="414" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">API key</text>
  <text x="552" y="432" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="500" fill="currentColor" fill-opacity="0.55">gateway_id · chain · validators</text>
  <text x="552" y="450" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="500" fill="currentColor" fill-opacity="0.4">(exchanged for short-lived JWT)</text>
  <path d="M 140,510 L 320,510 A 20,20 0 0 1 340,530 L 340,670 A 20,20 0 0 1 320,690 L 140,690 A 20,20 0 0 1 120,670 L 120,530 A 20,20 0 0 1 140,510 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="230" y="544" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Onboarding</text>
  <text x="230" y="582" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Partner Console</text>
  <text x="230" y="622" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">console.getoptimum.io</text>
  <text x="230" y="644" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">API key issuance</text>
</svg>

* The Gateway subscribes to ETH CL topics (beacon_block + all 64 attestation subnets) and forwards messages to the Optimum mump2p network.
* **Peer discovery and fork digest** are handled automatically. The gateway connects to the Optimum network on startup.
* **Identity, chain, and validator scope** all come from your **API key** - there is no per-network YAML to edit.
* **Config:** See [Quick Start](01_quick_start.md) and [Configuration](02_configuration.md).

## Requirements

* **CL Client**: Prysm, Lighthouse, Teku, Nimbus, or Lodestar running
* **API key**: Issued from the [Optimum Partner Console](https://console.getoptimum.io/) after onboarding (see [Quick Start](01_quick_start.md#generate-your-api-key))
* **Docker**: Docker Desktop or Docker Engine
* **Firewall**: Required ports open (see [Network Requirements](00_network_requirements.md))

## Getting Started

Follow the [**Quick Start**](01_quick_start.md) to get running in minutes.
