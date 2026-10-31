<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatAbsolute, formatRelative } from '../../lib/format'
  import type { Device, NetworkSection } from '../../lib/types'
  import {
    buildTopology,
    layoutTopology,
    SELF_ID,
    type TopologyEdge,
    type TopologyNode,
    type TopologyPoint
  } from '../../lib/topology'
  import EmptyState from '../ui/EmptyState.svelte'
  import Icon from '../ui/Icon.svelte'

  export let device: Device
  export let devices: Device[] = []
  export let onSelectDevice: ((id: string) => void) | null = null

  // The map uses the canvas's own pixel size as its coordinate system, so a
  // node lands where the layout put it instead of being letterboxed inside a
  // fixed viewBox. The fallback is only used before the first measurement.
  let canvasWidth = 0
  let canvasHeight = 0
  $: layoutWidth = canvasWidth > 240 ? canvasWidth : 900
  $: layoutHeight = canvasHeight > 240 ? canvasHeight : 620

  let snapshot: NetworkSection | null = null
  let loaded = false
  let loading = false
  let loadError = ''
  let timer: ReturnType<typeof setInterval> | null = null
  let mounted = false
  let loadedDeviceId = ''

  let selectedId: string | null = null
  let pinned: Record<string, TopologyPoint> = {}
  let view = { x: 0, y: 0, k: 1 }
  let hover: { node: TopologyNode; x: number; y: number } | null = null

  let shellEl: HTMLDivElement
  let svgEl: SVGSVGElement

  type DragState = { id: string; dx: number; dy: number; moved: boolean }
  let drag: DragState | null = null
  type PanState = { startX: number; startY: number; originX: number; originY: number; moved: boolean }
  let pan: PanState | null = null

  // A graph is rebuilt whenever the snapshot, the device or the fleet changes.
  // It is independent of the viewport, so panning and hovering never re-run
  // the layout.
  $: graph = buildTopology(device, snapshot, devices)
  $: baseLayout = layoutTopology(graph, layoutWidth, layoutHeight)
  $: points = new Map<string, TopologyPoint>(
    graph.nodes.map((node) => [node.id, pinned[node.id] ?? baseLayout.get(node.id) ?? { x: layoutWidth / 2, y: layoutHeight / 2 }])
  )

  $: selectedNode = selectedId ? graph.nodes.find((node) => node.id === selectedId && node.kind !== 'network') ?? null : null
  $: selectedNetwork = selectedId ? graph.networks.find((network) => network.id === selectedId) ?? null : null
  $: focusedNetworkId = selectedNetwork?.id ?? (selectedNode && selectedNode.kind !== 'internet' ? selectedNode.networkId : null)
  $: collectedAt = snapshot?.topology?.collected_at ?? ''
  $: snapshotAge = collectedAt ? formatRelative(collectedAt) : 'never'
  $: snapshotStale = Boolean(collectedAt) && Date.now() - Date.parse(collectedAt) > 6 * 60_000
  $: overlayCount = graph.networks.filter((network) => network.kind === 'tailscale').length
  $: lanCount = graph.networks.filter((network) => network.kind === 'lan').length
  $: hasMap = graph.networks.length > 0

  $: if (mounted && device.id !== loadedDeviceId) {
    loadedDeviceId = device.id
    resetForDevice()
    void load()
  }

  onMount(() => {
    mounted = true
    loadedDeviceId = device.id
    void load()
    // The agent refreshes the snapshot on its five-minute inventory cycle.
    // Polling once a minute keeps an open screen from showing a stale map
    // without hitting the server harder than the data changes.
    timer = setInterval(() => void load(), 60_000)
  })

  onDestroy(() => {
    if (timer) clearInterval(timer)
  })

  function resetForDevice() {
    snapshot = null
    loaded = false
    loadError = ''
    selectedId = null
    pinned = {}
    hover = null
    view = { x: 0, y: 0, k: 1 }
  }

  async function load() {
    if (loading) return
    loading = true
    try {
      const res = await apiFetch(apiURL(`/api/devices/${device.id}/network`))
      if (!res.ok) throw new Error(`network request failed (${res.status})`)
      snapshot = await res.json()
      loadError = ''
    } catch (e: any) {
      loadError = e?.message || 'Unable to load the network snapshot'
    } finally {
      loaded = true
      loading = false
    }
  }

  function selectNetwork(id: string) {
    selectedId = selectedId === id ? null : id
  }

  function selectNode(node: TopologyNode) {
    selectedId = node.id
  }

  function clearSelection() {
    selectedId = null
  }

  function isNetworkActive(id: string): boolean {
    return selectedId === id || selectedNode?.networkId === id
  }

  function isDimmed(node: TopologyNode): boolean {
    if (!focusedNetworkId) return false
    if (node.kind === 'self') return false
    return node.networkId !== focusedNetworkId && node.id !== focusedNetworkId
  }

  function edgeGateway(edge: TopologyEdge): TopologyNode | undefined {
    return edge.kind === 'route' ? graph.nodes.find((node) => node.id === edge.from) : undefined
  }

  function isEdgeActive(edge: TopologyEdge): boolean {
    if (!focusedNetworkId) return false
    if (edge.kind === 'route') return edgeGateway(edge)?.networkId === focusedNetworkId
    return edge.from === focusedNetworkId || edge.to === focusedNetworkId
  }

  function isEdgeDimmed(edge: TopologyEdge): boolean {
    if (!focusedNetworkId) return false
    if (edge.kind === 'membership') return edge.to !== focusedNetworkId
    if (edge.kind === 'route') return edgeGateway(edge)?.networkId !== focusedNetworkId
    return edge.from !== focusedNetworkId
  }

  // -- viewport -------------------------------------------------------------

  function svgPoint(event: { clientX: number; clientY: number }): TopologyPoint {
    if (!svgEl?.getScreenCTM) return { x: layoutWidth / 2, y: layoutHeight / 2 }
    const point = svgEl.createSVGPoint()
    point.x = event.clientX
    point.y = event.clientY
    const matrix = svgEl.getScreenCTM()
    if (!matrix) return { x: layoutWidth / 2, y: layoutHeight / 2 }
    const mapped = point.matrixTransform(matrix.inverse())
    return { x: mapped.x, y: mapped.y }
  }

  function toWorld(event: { clientX: number; clientY: number }): TopologyPoint {
    const point = svgPoint(event)
    return { x: (point.x - view.x) / view.k, y: (point.y - view.y) / view.k }
  }

  function zoomAt(factor: number, cx = layoutWidth / 2, cy = layoutHeight / 2) {
    const k = Math.min(2.6, Math.max(0.35, view.k * factor))
    if (k === view.k) return
    view = {
      k,
      x: cx - ((cx - view.x) * k) / view.k,
      y: cy - ((cy - view.y) * k) / view.k
    }
  }

  function zoomBy(factor: number) {
    zoomAt(factor, layoutWidth / 2, layoutHeight / 2)
  }

  function onWheel(event: WheelEvent) {
    const point = svgPoint(event)
    zoomAt(event.deltaY < 0 ? 1.12 : 1 / 1.12, point.x, point.y)
  }

  function resetView() {
    view = { x: 0, y: 0, k: 1 }
    pinned = {}
  }

  function startPan(event: PointerEvent) {
    if (event.button !== 0) return
    const point = svgPoint(event)
    pan = { startX: point.x, startY: point.y, originX: view.x, originY: view.y, moved: false }
    hover = null
    svgEl.setPointerCapture?.(event.pointerId)
  }

  function startNodeDrag(event: PointerEvent, node: TopologyNode) {
    if (event.button !== 0) return
    event.stopPropagation()
    event.preventDefault()
    // A hub is a structural anchor, not a movable node: the members spring to
    // it, so dragging it would leave them behind. A click selects it instead.
    if (node.kind === 'network') {
      selectNetwork(node.id)
      return
    }
    const point = toWorld(event)
    const current = points.get(node.id) ?? { x: layoutWidth / 2, y: layoutHeight / 2 }
    drag = { id: node.id, dx: current.x - point.x, dy: current.y - point.y, moved: false }
    hover = null
    ;(event.currentTarget as Element | null)?.setPointerCapture?.(event.pointerId)
  }

  function onPointerMove(event: PointerEvent) {
    if (drag) {
      const point = toWorld(event)
      pinned = { ...pinned, [drag.id]: { x: point.x + drag.dx, y: point.y + drag.dy } }
      drag.moved = true
      return
    }
    if (pan) {
      const point = svgPoint(event)
      pan.moved = true
      view = {
        ...view,
        x: pan.originX + (point.x - pan.startX),
        y: pan.originY + (point.y - pan.startY)
      }
    }
  }

  function onPointerUp() {
    if (drag) {
      if (!drag.moved) {
        const node = graph.nodes.find((candidate) => candidate.id === drag?.id)
        if (node) selectNode(node)
      }
      drag = null
    }
    if (pan && !pan.moved) clearSelection()
    pan = null
  }

  // -- drawing helpers ------------------------------------------------------

  function edgePath(from: TopologyPoint, to: TopologyPoint): string {
    const dx = to.x - from.x
    const dy = to.y - from.y
    const length = Math.hypot(dx, dy)
    if (length < 90) return `M ${round(from.x)} ${round(from.y)} L ${round(to.x)} ${round(to.y)}`
    const curve = Math.min(34, length * 0.09)
    const mx = (from.x + to.x) / 2 - (dy / length) * curve
    const my = (from.y + to.y) / 2 + (dx / length) * curve
    return `M ${round(from.x)} ${round(from.y)} Q ${round(mx)} ${round(my)} ${round(to.x)} ${round(to.y)}`
  }

  function round(value: number): number {
    return Math.round(value * 10) / 10
  }

  function nodeRadius(node: TopologyNode): number {
    if (node.kind === 'self') return 24
    if (node.kind === 'internet') return 20
    return 18
  }

  function truncate(value: string, length: number): string {
    if (!value) return ''
    return value.length > length ? `${value.slice(0, length - 1)}…` : value
  }

  function glyphFor(node: TopologyNode): string {
    if (node.kind === 'self') return 'M3 5h18v11H3zM8 20h8M12 16v4'
    if (node.kind === 'gateway') return 'M4 13h16v6H4zM7 9a6 6 0 0 1 10 0M9 6a9 9 0 0 1 6 0'
    if (node.kind === 'internet') return 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18'
    if (node.kind === 'peer') return osGlyph(node.os)
    return 'M4 5h16v10H4zM9 19h6M12 15v4'
  }

  function osGlyph(os?: string): string {
    switch ((os ?? '').toLowerCase()) {
      case 'linux':
        return 'M5 5l4 4-4 4M12 13h7'
      case 'windows':
        return 'M4 4h6v6H4zM13 4h7v6h-7zM4 13h6v7H4zM13 13h7v7h-7z'
      case 'macos':
      case 'ios':
        return 'M4 5h12v8H4zM2 14h16'
      case 'android':
        return 'M8 3h8v14H8zM8 20h.01M16 20h.01'
      default:
        return 'M4 5h16v10H4zM9 19h6M12 15v4'
    }
  }

  function toneClass(tone: string): string {
    return `tone-${tone}`
  }

  function stateLabel(node: TopologyNode): string {
    if (node.kind === 'peer') return node.online ? (node.direct ? 'online · direct' : 'online · relayed') : 'offline'
    if (node.state === 'reachable') return 'reachable now'
    if (node.state === 'stale') return 'not confirmed recently'
    if (node.state === 'failed') return 'did not answer'
    return node.online ? 'online' : 'offline'
  }

  function showTip(event: PointerEvent, node: TopologyNode) {
    if (!shellEl) return
    const rect = shellEl.getBoundingClientRect()
    hover = {
      node,
      x: Math.min(rect.width - 230, Math.max(8, event.clientX - rect.left + 14)),
      y: Math.min(rect.height - 110, Math.max(8, event.clientY - rect.top + 14))
    }
  }

  function openManagedDevice(node: TopologyNode) {
    if (node.managedDeviceId) onSelectDevice?.(node.managedDeviceId)
  }

  function networkTone(id: string): string {
    const network = graph.networks.find((candidate) => candidate.id === id)
    if (!network) return 'offline'
    if (network.error) return 'crit'
    if (network.kind === 'tailscale') return network.state === 'running' ? 'ok' : 'offline'
    return network.state === 'down' ? 'offline' : 'ok'
  }
</script>

<section class="topology">
  <header class="head">
    <div class="intro">
      <h2>Network map</h2>
      <p>Every network this device takes part in, and every node it can see right now.</p>
    </div>
    <div class="head-actions">
      <span class="snapshot" class:stale={snapshotStale} title={formatAbsolute(collectedAt)}>
        Snapshot {snapshotAge}
      </span>
      <button class="btn" on:click={load} disabled={loading} title="Fetch the latest snapshot">
        <Icon name="refresh" size={13} />
        {loading ? 'Loading' : 'Refresh'}
      </button>
      <button class="btn ghost" on:click={resetView} title="Reset zoom and node positions">Reset view</button>
    </div>
  </header>

  {#if loadError}
    <div class="error-banner" role="alert">
      <span>{loadError}</span>
      <button class="retry" on:click={load}>Retry</button>
    </div>
  {/if}

  <div class="kpis">
    <article class="kpi">
      <span class="label">Networks</span>
      <span class="value">{graph.stats.networks}</span>
      <span class="sub">{lanCount} LAN · {overlayCount} overlay</span>
    </article>
    <article class="kpi">
      <span class="label">Visible devices</span>
      <span class="value">{graph.stats.devices}</span>
      <span class="sub">{graph.stats.online} online · {graph.stats.offline} offline</span>
    </article>
    <article class="kpi">
      <span class="label">Local segment</span>
      <span class="value">{graph.stats.lan}</span>
      <span class="sub">from the kernel neighbour table</span>
    </article>
    <article class="kpi">
      <span class="label">Overlay peers</span>
      <span class="value">{graph.stats.peers}</span>
      <span class="sub">{overlayCount > 0 ? 'reported by the overlay daemon' : 'no overlay detected'}</span>
    </article>
  </div>

  {#if !loaded}
    <div class="loading-card">Loading the network snapshot…</div>
  {:else if !hasMap}
    <EmptyState
      icon="globe"
      title="No network map yet"
      message="The agent reports the topology on its five-minute inventory cycle. A device with no interface address, no default route and no overlay has no network to draw."
      actionLabel="Check again"
      onAction={load}
    />
  {:else}
    <div class="network-strip">
      {#each graph.networks as network (network.id)}
        <button
          class="network-chip"
          class:active={isNetworkActive(network.id)}
          class:bad={Boolean(network.error)}
          on:click={() => selectNetwork(network.id)}
        >
          <span class="network-icon {network.kind}">
            <Icon name={network.kind === 'tailscale' ? 'shield' : 'network'} size={14} />
          </span>
          <span class="network-copy">
            <strong>{network.title}</strong>
            <span>{network.subtitle}</span>
          </span>
          <span class="network-count">{network.nodeIds.length}</span>
          <i class="dot {networkTone(network.id)}"></i>
        </button>
      {/each}
      <span class="strip-hint">Click a network to focus it · click a node for details</span>
    </div>

    <div class="map-layout">
      <div class="map-card">
        <div class="map-toolbar">
          <span class="legend"><i class="dot ok"></i>reachable</span>
          <span class="legend"><i class="dot warn"></i>not confirmed</span>
          <span class="legend"><i class="dot offline"></i>offline</span>
          <span class="legend"><i class="line lan"></i>LAN</span>
          <span class="legend"><i class="line ts"></i>overlay path</span>
          <span class="hint">Drag a node to pin it · scroll to zoom</span>
        </div>

        <div class="canvas" bind:this={shellEl} bind:clientWidth={canvasWidth} bind:clientHeight={canvasHeight}>
          <svg
            bind:this={svgEl}
            viewBox="0 0 {layoutWidth} {layoutHeight}"
            role="application"
            aria-label="Network map"
            on:pointerdown={startPan}
            on:pointermove={onPointerMove}
            on:pointerup={onPointerUp}
            on:pointerleave={onPointerUp}
            on:pointercancel={onPointerUp}
            on:wheel|nonpassive|preventDefault={onWheel}
          >
            <defs>
              <pattern id="topology-grid" width="32" height="32" patternUnits="userSpaceOnUse">
                <circle cx="1" cy="1" r="1" fill="rgba(148, 163, 184, 0.09)" />
              </pattern>
              <radialGradient id="topology-glow" cx="50%" cy="50%" r="50%">
                <stop offset="0%" stop-color="rgba(52, 211, 153, 0.07)" />
                <stop offset="100%" stop-color="rgba(52, 211, 153, 0)" />
              </radialGradient>
            </defs>

            <rect width={layoutWidth} height={layoutHeight} fill="url(#topology-grid)" />
            <circle cx={layoutWidth / 2} cy={layoutHeight / 2} r={Math.min(layoutWidth, layoutHeight) * 0.42} fill="url(#topology-glow)" />

            <g transform="translate({view.x} {view.y}) scale({view.k})">
              {#each graph.edges as edge (edge.id)}
                {@const from = points.get(edge.from)}
                {@const to = points.get(edge.to)}
                {#if from && to}
                  <path
                    class="edge {edge.kind}"
                    class:direct={edge.direct}
                    class:active={isEdgeActive(edge)}
                    class:dimmed={isEdgeDimmed(edge)}
                    d={edgePath(from, to)}
                  />
                {/if}
              {/each}

              {#each graph.nodes as node (node.id)}
                {@const point = points.get(node.id)}
                {#if point}
                  {#if node.kind === 'network'}
                    <g
                      class="hub"
                      class:overlay={node.id.startsWith('overlay:tailscale')}
                      class:selected={isNetworkActive(node.id)}
                      class:dimmed={isDimmed(node)}
                      class:bad={Boolean(graph.networks.find((network) => network.id === node.id)?.error)}
                      transform="translate({point.x} {point.y})"
                      role="button"
                      tabindex="0"
                      aria-label="Network {node.label} {node.sublabel}"
                      on:pointerdown={(event) => startNodeDrag(event, node)}
                      on:pointerenter={(event) => showTip(event, node)}
                      on:pointerleave={() => (hover = null)}
                      on:keydown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          selectNetwork(node.id)
                        }
                      }}
                    >
                      <rect x="-78" y="-27" width="156" height="54" rx="14" />
                      <text class="hub-title" y="-4">{truncate(node.label, 18)}</text>
                      <text class="hub-sub" y="13">{truncate(node.sublabel, 24)}</text>
                      <circle class="status {toneClass(node.tone)}" cx="65" cy="-17" r="4.5" />
                    </g>
                  {:else}
                    <g
                      class="node {node.kind}"
                      class:selected={selectedId === node.id}
                      class:dimmed={isDimmed(node)}
                      class:offline={!node.online}
                      class:managed={Boolean(node.managedDeviceId)}
                      transform="translate({point.x} {point.y})"
                      role="button"
                      tabindex="0"
                      aria-label={node.label}
                      on:pointerdown={(event) => startNodeDrag(event, node)}
                      on:pointerenter={(event) => showTip(event, node)}
                      on:pointerleave={() => (hover = null)}
                      on:keydown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          selectNode(node)
                        }
                      }}
                    >
                      {#if node.kind === 'self'}
                        <circle class="halo" r={nodeRadius(node) + 12} />
                      {/if}
                      <circle class="body" r={nodeRadius(node)} />
                      <path class="glyph" d={glyphFor(node)} transform="translate(-12,-12) scale(1)" />
                      <circle class="status {toneClass(node.tone)}" cx={nodeRadius(node) * 0.72} cy={-nodeRadius(node) * 0.72} r="4.5" />
                      {#if node.online || node.kind !== 'peer' || selectedId === node.id || hover?.node.id === node.id}
                        <text class="node-label" y={nodeRadius(node) + 17}>{truncate(node.label, 20)}</text>
                      {/if}
                    </g>
                  {/if}
                {/if}
              {/each}
            </g>
          </svg>

          <div class="zoom-controls">
            <button on:click={() => zoomBy(1.2)} aria-label="Zoom in">+</button>
            <button on:click={() => zoomBy(1 / 1.2)} aria-label="Zoom out">−</button>
            <button on:click={resetView} aria-label="Reset view">⤢</button>
          </div>

          {#if hover}
            <div class="tooltip" style="left: {hover.x}px; top: {hover.y}px">
              <strong>{hover.node.label}</strong>
              <span>{hover.node.sublabel}</span>
              {#if hover.node.ips.length > 0}<span class="mono">{hover.node.ips.join(' · ')}</span>{/if}
              <span class="tooltip-state {toneClass(hover.node.tone)}">{stateLabel(hover.node)}</span>
              {#if hover.node.managedDeviceLabel}<span class="managed-badge">SyncWin device</span>{/if}
            </div>
          {/if}
        </div>
      </div>

      {#if selectedId}
      <aside class="rail">
        {#if selectedNode}
          <div class="detail">
            <header class="detail-head">
              <span class="detail-kind">{selectedNode.kind === 'peer' ? 'Overlay node' : selectedNode.kind === 'gateway' ? 'Gateway' : selectedNode.kind === 'neighbour' ? 'LAN node' : selectedNode.kind === 'self' ? 'This device' : 'Internet'}</span>
              <button class="close" on:click={clearSelection} aria-label="Close details"><Icon name="x" size={13} /></button>
            </header>
            <h3 class="detail-title">{selectedNode.label}</h3>
            <p class="detail-sub">{selectedNode.sublabel}</p>

            <div class="badges">
              <span class="badge {toneClass(selectedNode.tone)}">{stateLabel(selectedNode)}</span>
              {#if selectedNode.managedDeviceLabel}<span class="badge accent">SyncWin device</span>{/if}
              {#if selectedNode.exitNode}<span class="badge">Exit node</span>{/if}
              {#if selectedNode.exitNodeOption && !selectedNode.exitNode}<span class="badge">Can be exit node</span>{/if}
            </div>

            <dl class="facts">
              {#if selectedNode.ips.length > 0}
                <div><dt>Address</dt><dd class="mono">{selectedNode.ips.join(', ')}</dd></div>
              {/if}
              {#if selectedNode.mac}
                <div><dt>MAC</dt><dd class="mono">{selectedNode.mac}</dd></div>
              {/if}
              {#if selectedNode.vendor}
                <div><dt>Vendor</dt><dd>{selectedNode.vendor === 'local' ? 'locally administered' : selectedNode.vendor}</dd></div>
              {/if}
              {#if selectedNode.iface}
                <div><dt>Interface</dt><dd class="mono">{selectedNode.iface}</dd></div>
              {/if}
              {#if selectedNode.os}
                <div><dt>System</dt><dd>{selectedNode.os}</dd></div>
              {/if}
              {#if selectedNode.kind === 'peer'}
                <div><dt>Path</dt><dd>{selectedNode.direct ? 'direct' : selectedNode.relay ? `relayed via ${selectedNode.relay}` : 'relayed'}</dd></div>
              {/if}
              {#if selectedNode.relay && selectedNode.direct}
                <div><dt>Fallback relay</dt><dd>{selectedNode.relay}</dd></div>
              {/if}
              {#if selectedNode.lastSeen && !selectedNode.online}
                <div><dt>Last seen</dt><dd>{formatRelative(selectedNode.lastSeen)}</dd></div>
              {/if}
              {#if selectedNode.tags && selectedNode.tags.length > 0}
                <div><dt>Tags</dt><dd class="mono">{selectedNode.tags.join(', ')}</dd></div>
              {/if}
            </dl>

            {#if selectedNode.managedDeviceId && onSelectDevice}
              <button class="primary-btn" on:click={() => openManagedDevice(selectedNode)}>
                Open device
              </button>
            {/if}
          </div>
        {:else if selectedNetwork}
          <div class="detail">
            <header class="detail-head">
              <span class="detail-kind">{selectedNetwork.kind === 'tailscale' ? 'Overlay network' : 'Local network'}</span>
              <button class="close" on:click={clearSelection} aria-label="Close details"><Icon name="x" size={13} /></button>
            </header>
            <h3 class="detail-title">{selectedNetwork.title}</h3>
            <p class="detail-sub">{selectedNetwork.subtitle}</p>

            <div class="badges">
              <span class="badge {networkTone(selectedNetwork.id)}">{selectedNetwork.state}</span>
              {#if selectedNetwork.cidr}<span class="badge mono">{selectedNetwork.cidr}</span>{/if}
            </div>

            {#if selectedNetwork.error}
              <p class="detail-error">{selectedNetwork.error}</p>
            {/if}

            <dl class="facts">
              {#if selectedNetwork.iface}
                <div><dt>Interface</dt><dd class="mono">{selectedNetwork.iface}</dd></div>
              {/if}
              {#if selectedNetwork.address}
                <div><dt>This address</dt><dd class="mono">{selectedNetwork.address}</dd></div>
              {/if}
              {#if selectedNetwork.gateway}
                <div><dt>Gateway</dt><dd class="mono">{selectedNetwork.gateway}</dd></div>
              {/if}
              <div><dt>Nodes</dt><dd>{selectedNetwork.nodeIds.length}</dd></div>
            </dl>

            <h4 class="detail-subhead">Members</h4>
            {#if selectedNetwork.nodeIds.length === 0}
              <p class="detail-note">
                {selectedNetwork.kind === 'tailscale'
                  ? 'The overlay reports no peers. That is a real answer, not a failed read.'
                  : 'No neighbour has been seen on this interface yet. The kernel only knows devices this host has exchanged traffic with.'}
              </p>
            {:else}
              <ul class="members">
                {#each selectedNetwork.nodeIds as id (id)}
                  {@const member = graph.nodes.find((node) => node.id === id)}
                  {#if member}
                    <li>
                      <button class="member-row" on:click={() => selectNode(member)}>
                        <i class="dot {toneClass(member.tone)}"></i>
                        <span class="member-copy">
                          <strong>{member.label}</strong>
                          <span>{member.sublabel}</span>
                        </span>
                      </button>
                    </li>
                  {/if}
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </aside>
      {/if}
    </div>

    <div class="notes-grid">
      <div class="detail help">
        <h3 class="detail-title">How to read this map</h3>
        <ul class="help-list">
          <li><i class="node-swatch self"></i><span><b>This device</b> is the centre. It belongs to every network on the map.</span></li>
          <li><i class="node-swatch hub"></i><span><b>A network hub</b> groups one segment or overlay. Click it to list its members.</span></li>
          <li><i class="node-swatch gateway"></i><span><b>The gateway</b> is where the default route leaves, and the internet node is what it leads to.</span></li>
          <li><i class="node-swatch peer"></i><span><b>Overlay peers</b> come from the Tailscale daemon. Offline peers stay visible, greyed out and unlabelled; hover one to read its name.</span></li>
          <li><i class="node-swatch neighbour"></i><span><b>LAN nodes</b> are the kernel's ARP/NDP neighbour table: devices this host has talked to recently.</span></li>
        </ul>
        <p class="help-note">Click any node for its addresses, MAC vendor, path and tags.</p>
      </div>

      {#if graph.unreadable.length > 0}
        <div class="detail note">
          <h3 class="detail-title">Tunnels without a peer list</h3>
          <ul class="tunnel-list">
            {#each graph.unreadable as tunnel (tunnel.interface)}
              <li><span class="mono">{tunnel.interface}</span><span>{tunnel.kind}</span><span class="tunnel-state">{tunnel.state || 'unknown'}</span></li>
            {/each}
          </ul>
          <p class="detail-note">
            WireGuard and similar daemons keep their peer configuration under root. The agent runs unprivileged,
            so the interface is shown while its peers are not readable.
          </p>
        </div>
      {/if}
    </div>

    <footer class="footnotes">
      <p><b>Freshness.</b> This map is a snapshot, not a live scan. The agent reads the neighbour table, the routing table and the Tailscale local API on its five-minute inventory cycle.</p>
      <p><b>What is not here.</b> Devices that have never exchanged traffic with this host do not appear in ARP. A port scan or a ping sweep is not attempted, because the agent must not probe a network it was not asked to probe.</p>
    </footer>
  {/if}
</section>

<style>
  .topology { display: grid; gap: 0.85rem; }

  .head { display: flex; align-items: flex-end; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
  .intro h2 { margin: 0; font-size: 1rem; font-weight: 600; color: var(--text-bright); }
  .intro p { margin: 0.2rem 0 0; color: var(--text-muted); font-size: 0.78rem; }
  .head-actions { display: flex; align-items: center; gap: 0.45rem; }
  .snapshot { color: var(--text-faint); font-size: 0.7rem; margin-right: 0.2rem; }
  .snapshot.stale { color: var(--warn); }

  .btn {
    display: inline-flex; align-items: center; gap: 0.35rem;
    padding: 0.4rem 0.65rem; border: 1px solid var(--border); border-radius: var(--radius-sm);
    background: var(--card); color: var(--text); font: inherit; font-size: 0.73rem; cursor: pointer;
    transition: border-color var(--t-fast), background var(--t-fast);
  }
  .btn:hover { border-color: var(--accent-border); background: var(--card-hover); }
  .btn:disabled { opacity: 0.55; cursor: default; }
  .btn.ghost { background: transparent; color: var(--text-muted); }

  .error-banner {
    display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.75rem;
    border: 1px solid rgba(248, 113, 113, 0.35); border-radius: var(--radius-sm);
    background: var(--crit-dim); color: var(--crit); font-size: 0.76rem;
  }
  .retry { margin-left: auto; border: 1px solid var(--border); background: transparent; color: inherit; border-radius: var(--radius-sm); padding: 0.2rem 0.5rem; font: inherit; cursor: pointer; }

  .kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 0.6rem; }
  .kpi { display: grid; gap: 0.1rem; padding: 0.65rem 0.8rem; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .kpi .value { font-size: 1.55rem; }
  .kpi .sub { color: var(--text-faint); font-size: 0.68rem; }

  .loading-card { padding: 3rem 1rem; text-align: center; color: var(--text-muted); font-size: 0.82rem; border: 1px dashed var(--border-strong); border-radius: var(--radius); }

  .map-layout { position: relative; }

  .network-strip { display: flex; gap: 0.45rem; align-items: stretch; overflow-x: auto; padding-bottom: 2px; }
  .network-chip {
    display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; align-items: center; gap: 0.5rem;
    min-width: 220px; padding: 0.45rem 0.55rem; border: 1px solid var(--border); border-radius: var(--radius-sm);
    background: var(--card); color: var(--text); font: inherit; text-align: left; cursor: pointer;
    transition: border-color var(--t-fast), background var(--t-fast);
  }
  .network-chip:hover { border-color: var(--border-strong); }
  .network-chip.active { border-color: var(--accent-border); background: var(--accent-dim); }
  .network-chip.bad { border-color: rgba(248, 113, 113, 0.4); }
  .strip-hint { align-self: center; margin-left: auto; padding-left: 0.4rem; color: var(--text-faint); font-size: 0.66rem; white-space: nowrap; }

  .map-card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .map-toolbar {
    display: flex; align-items: center; gap: 0.75rem; flex-wrap: wrap;
    padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--border);
    color: var(--text-faint); font-size: 0.66rem;
  }
  .legend { display: inline-flex; align-items: center; gap: 0.35rem; }
  .legend .line { width: 16px; height: 0; border-top: 2px solid var(--border-strong); }
  .legend .line.ts { border-top: 2px dashed var(--series-1); }
  .legend .line.lan { border-top: 2px solid rgba(147, 152, 166, 0.5); }
  .hint { margin-left: auto; color: var(--text-faint); }

  .canvas { position: relative; height: clamp(440px, calc(100vh - 400px), 680px); cursor: grab; }
  .canvas:active { cursor: grabbing; }
  .canvas svg { display: block; width: 100%; height: 100%; touch-action: none; user-select: none; }

  .edge { fill: none; stroke: rgba(147, 152, 166, 0.28); stroke-width: 1.1; transition: opacity var(--t-fast); }
  .edge.membership { stroke: var(--border-strong); stroke-width: 1.5; }
  .edge.tailscale { stroke: rgba(96, 165, 250, 0.5); stroke-dasharray: 5 5; }
  .edge.tailscale.direct { stroke: rgba(96, 165, 250, 0.78); stroke-dasharray: 8 4; }
  .edge.route { stroke: rgba(34, 211, 238, 0.3); stroke-dasharray: 2 7; }
  .edge.active { stroke: var(--accent); stroke-width: 2; }
  .edge.dimmed { opacity: 0.18; }

  .node { cursor: grab; transition: opacity var(--t-fast); }
  .node .body {
    fill: var(--bg-elevated); stroke: var(--border-strong); stroke-width: 1.5;
    transition: stroke var(--t-fast), fill var(--t-fast);
  }
  .node .glyph { fill: none; stroke: var(--text-muted); stroke-width: 1.4; stroke-linecap: round; stroke-linejoin: round; pointer-events: none; }
  .node .halo { fill: var(--accent-dim); stroke: none; }
  .node.self .body { fill: rgba(52, 211, 153, 0.12); stroke: var(--accent); }
  .node.self .glyph { stroke: var(--accent); }
  .node.gateway .body { fill: rgba(34, 211, 238, 0.08); stroke: rgba(34, 211, 238, 0.55); }
  .node.gateway .glyph { stroke: var(--series-4); }
  .node.internet .body { fill: rgba(148, 163, 184, 0.06); stroke: rgba(148, 163, 184, 0.4); }
  .node.peer .body { fill: rgba(96, 165, 250, 0.07); stroke: rgba(96, 165, 250, 0.5); }
  .node.peer .glyph { stroke: var(--series-1); }
  .node.managed .body { stroke-width: 2.5; }
  .node.offline .body { stroke: var(--border); }
  .node.offline .glyph { stroke: var(--text-faint); }
  .node.dimmed { opacity: 0.25; }
  .node.selected .body { stroke: var(--accent); stroke-width: 2.5; filter: drop-shadow(0 0 6px rgba(52, 211, 153, 0.5)); }
  .node:hover .body { stroke: var(--text); }

  .node-label { fill: var(--text); font-size: 11px; font-weight: 600; text-anchor: middle; pointer-events: none; }

  .status { stroke: none; }
  .tone-ok { fill: var(--ok); }
  .tone-warn { fill: var(--warn); }
  .tone-crit { fill: var(--crit); }
  .tone-offline { fill: var(--offline); }

  .hub { cursor: pointer; transition: opacity var(--t-fast); }
  .hub rect {
    fill: var(--card-inset); stroke: var(--border-strong); stroke-width: 1.2;
    transition: stroke var(--t-fast);
  }
  .hub:hover rect { stroke: var(--text-muted); }
  .hub.selected rect { stroke: var(--accent); stroke-width: 2; filter: drop-shadow(0 0 8px rgba(52, 211, 153, 0.25)); }
  .hub.overlay rect { stroke: rgba(96, 165, 250, 0.45); }
  .hub.bad rect { stroke: rgba(248, 113, 113, 0.55); fill: var(--crit-dim); }
  .hub.dimmed { opacity: 0.25; }
  .hub-title { fill: var(--text); font-size: 11px; font-weight: 700; text-anchor: middle; letter-spacing: 0.02em; }
  .hub-sub { fill: var(--text-faint); font-size: 8.5px; text-anchor: middle; font-family: var(--mono); }

  .zoom-controls { position: absolute; right: 0.6rem; bottom: 0.6rem; display: grid; gap: 0.25rem; }
  .zoom-controls button {
    width: 26px; height: 26px; display: grid; place-items: center;
    border: 1px solid var(--border); border-radius: 7px; background: rgba(17, 17, 19, 0.9);
    color: var(--text-muted); font: inherit; font-size: 0.9rem; cursor: pointer;
  }
  .zoom-controls button:hover { color: var(--text); border-color: var(--accent-border); }

  .tooltip {
    position: absolute; z-index: 5; width: 220px; display: grid; gap: 0.15rem;
    padding: 0.55rem 0.65rem; background: rgba(10, 10, 11, 0.96);
    border: 1px solid var(--border-strong); border-radius: var(--radius-sm); box-shadow: var(--shadow-pop);
    pointer-events: none; font-size: 0.7rem; color: var(--text-muted);
  }
  .tooltip strong { color: var(--text-bright); font-size: 0.78rem; }
  .tooltip .mono { color: var(--text); font-size: 0.66rem; }
  .tooltip-state { font-size: 0.66rem; }
  .tooltip-state.tone-ok { color: var(--ok); }
  .tooltip-state.tone-warn { color: var(--warn); }
  .tooltip-state.tone-crit { color: var(--crit); }
  .tooltip-state.tone-offline { color: var(--text-faint); }
  .managed-badge { color: var(--accent); font-size: 0.64rem; font-weight: 600; }

  .rail {
    position: absolute; top: 0.65rem; right: 0.65rem; z-index: 8;
    display: grid; gap: 0.6rem; width: min(340px, calc(100% - 1.3rem));
    max-height: calc(100% - 1.3rem); overflow-y: auto; padding-right: 2px;
  }
  .rail .detail { background: rgba(10, 10, 11, 0.96); backdrop-filter: blur(10px); box-shadow: var(--shadow-pop); }
  .detail {
    background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 0.75rem;
  }
  .network-icon { width: 24px; height: 24px; display: grid; place-items: center; border-radius: 7px; background: var(--bg-elevated); color: var(--text-muted); }
  .network-icon.tailscale { color: var(--series-1); }
  .network-copy { display: grid; min-width: 0; }
  .network-copy strong { font-size: 0.75rem; font-weight: 600; color: var(--text); }
  .network-copy span { font-size: 0.66rem; color: var(--text-faint); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .network-count { font-size: 0.68rem; color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; flex: none; }
  .dot.ok { background: var(--ok); }
  .dot.warn { background: var(--warn); }
  .dot.crit { background: var(--crit); }
  .dot.offline { background: var(--offline); }
  .network-error { margin: 0.15rem 0 0.3rem 0.4rem; color: var(--crit); font-size: 0.65rem; line-height: 1.4; }

  .detail-head { display: flex; align-items: center; justify-content: space-between; }
  .detail-kind { color: var(--text-faint); font-size: 0.64rem; text-transform: uppercase; letter-spacing: 0.06em; }
  .close { border: 0; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 0.8rem; padding: 0.1rem; }
  .close:hover { color: var(--text); }
  .detail-title { margin: 0.35rem 0 0; font-size: 0.95rem; color: var(--text-bright); word-break: break-word; }
  .detail-sub { margin: 0.1rem 0 0.5rem; color: var(--text-muted); font-size: 0.72rem; word-break: break-word; }
  .badges { display: flex; flex-wrap: wrap; gap: 0.3rem; margin-bottom: 0.5rem; }
  .badge { padding: 0.12rem 0.4rem; border-radius: var(--radius-pill); border: 1px solid var(--border); color: var(--text-muted); font-size: 0.62rem; }
  .badge.accent { border-color: var(--accent-border); color: var(--accent); }
  .badge.tone-ok { border-color: rgba(52, 211, 153, 0.35); color: var(--ok); }
  .badge.tone-warn { border-color: rgba(251, 191, 36, 0.35); color: var(--warn); }
  .badge.tone-crit { border-color: rgba(248, 113, 113, 0.35); color: var(--crit); }
  .badge.tone-offline { color: var(--text-faint); }

  .facts { display: grid; gap: 0.32rem; margin: 0.5rem 0; }
  .facts > div { display: grid; grid-template-columns: 5.5rem 1fr; gap: 0.4rem; align-items: baseline; }
  .facts dt { color: var(--text-faint); font-size: 0.64rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.72rem; word-break: break-word; }
  .mono { font-family: var(--mono); font-size: 0.68rem; }

  .primary-btn {
    width: 100%; padding: 0.45rem 0.6rem; margin-top: 0.3rem;
    border: 1px solid var(--accent-border); border-radius: var(--radius-sm);
    background: var(--accent-dim); color: var(--accent); font: inherit; font-size: 0.72rem; font-weight: 600; cursor: pointer;
  }
  .primary-btn:hover { background: rgba(52, 211, 153, 0.25); }

  .detail-subhead { margin: 0.75rem 0 0.35rem; font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); }
  .members { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.15rem; }
  .member-row {
    display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 0.5rem;
    width: 100%; padding: 0.3rem 0.35rem; border: 1px solid transparent; border-radius: 6px;
    background: transparent; color: inherit; font: inherit; text-align: left; cursor: pointer;
  }
  .member-row:hover { border-color: var(--border); background: var(--card-inset); }
  .member-copy { display: grid; min-width: 0; }
  .member-copy strong { font-size: 0.71rem; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .member-copy span { font-size: 0.64rem; color: var(--text-faint); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .detail-note { margin: 0.35rem 0 0; color: var(--text-faint); font-size: 0.68rem; line-height: 1.5; }
  .detail-error { margin: 0.4rem 0; padding: 0.4rem 0.5rem; border: 1px solid rgba(248, 113, 113, 0.35); border-radius: var(--radius-sm); background: var(--crit-dim); color: var(--crit); font-size: 0.68rem; line-height: 1.45; }

  .help-list { list-style: none; margin: 0.5rem 0 0; padding: 0; display: grid; gap: 0.5rem; }
  .help-list li { display: grid; grid-template-columns: 18px minmax(0, 1fr); gap: 0.5rem; align-items: start; color: var(--text-muted); font-size: 0.7rem; line-height: 1.45; }
  .help-list b { color: var(--text); }
  .node-swatch { width: 12px; height: 12px; border-radius: 50%; margin-top: 0.18rem; border: 1.5px solid var(--border-strong); }
  .node-swatch.self { border-color: var(--accent); background: var(--accent-dim); }
  .node-swatch.hub { border-radius: 4px; border-color: var(--text-muted); }
  .node-swatch.gateway { border-color: var(--series-4); background: rgba(34, 211, 238, 0.12); }
  .node-swatch.peer { border-color: var(--series-1); background: rgba(96, 165, 250, 0.1); }
  .node-swatch.neighbour { border-color: var(--text-faint); }
  .help-note { margin: 0.6rem 0 0; color: var(--text-faint); font-size: 0.66rem; }

  .tunnel-list { list-style: none; margin: 0.45rem 0 0; padding: 0; display: grid; gap: 0.25rem; }
  .tunnel-list li { display: grid; grid-template-columns: 1fr auto auto; gap: 0.5rem; font-size: 0.68rem; color: var(--text); }
  .tunnel-list li span:not(.mono) { color: var(--text-muted); }
  .tunnel-state { color: var(--text-faint); }

  .notes-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 0.65rem; }

  .footnotes { display: grid; gap: 0.35rem; padding: 0.1rem 0.1rem 0.4rem; }
  .footnotes p { margin: 0; color: var(--text-faint); font-size: 0.68rem; line-height: 1.5; }
  .footnotes b { color: var(--text-muted); }

  @media (max-width: 980px) {
    .rail { position: static; width: auto; max-height: none; margin-top: 0.6rem; }
    .canvas { height: 520px; }
    .strip-hint { display: none; }
  }

  @media (max-width: 620px) {
    .head { align-items: flex-start; }
    .head-actions { width: 100%; flex-wrap: wrap; }
    .hint { display: none; }
    .canvas { height: 440px; }
  }
</style>
