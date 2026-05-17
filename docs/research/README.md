# Research & Design Documents

This directory contains active design documents and architectural research for the Gearbox platform.

## Contents

### Gear System Architecture

- **[gear-system.md](gear-system.md)**
  - Final gear system design
  - Architecture for both gearbox-agent and gearbox dashboard
  - Core requirements and principles

- **[gear-system-analysis.md](gear-system-analysis.md)** (27 KB)
  - Detailed feasibility study for gear architecture
  - Comparison of architectural approaches
  - Implementation recommendations and trade-offs

### Gear UX

- **[haproxy-dashboard-stack-layout.md](haproxy-dashboard-stack-layout.md)**
  - Redesign proposal for the HAProxy dashboard's stack-with-multiple-containers layout ([#79](https://github.com/sarg3nt/gearbox/issues/79))
  - Best-practice survey (Portainer, Lens, ArgoCD, Datadog, HAProxy stats, Grafana, NN/g)
  - Three layout options with ASCII mockups, recommendation, and implementation outline

- **[metrics-source-agnostic.md](metrics-source-agnostic.md)**
  - Plan to evolve the Metrics gear from "HAProxy with extras" to a source-agnostic dashboard with per-source attribution ([#87](https://github.com/sarg3nt/gearbox/issues/87))
  - Surveys the existing capability/probe + discovery infrastructure on the agent side
  - 9-phase rollout, each independently shippable, starting from a capability manifest endpoint and ending at multi-source (HAProxy + nginx + Apache + Caddy + Docker) drill-down

## Purpose

These documents serve as:

1. **Architectural Reference** - Core design decisions for the gear system
2. **Implementation Guide** - Reference when building new gears
3. **Design Rationale** - Understanding why the gear architecture was chosen
4. **Knowledge Base** - Onboarding developers to Gearbox's architecture

## Related Documentation

- **Completed work and migrations** - See [../reports/](../reports/)
- **Active development tasks** - See [TASKS.md](../../TASKS.md)
- **Gear documentation** - See [../gears.md](../gears.md)
