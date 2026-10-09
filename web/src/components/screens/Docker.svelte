<script lang="ts">
  import DockerTab from '../DockerTab.svelte'
  import type { Device } from '../../lib/types'

  // Docker used to open inside the device modal. It is now a first-class screen
  // so container management gets the full width and the modal can be retired.
  export let device: Device
  export let authHeaders: Record<string, string> = {}
</script>

<section class="docker">
  <header class="docker-head">
    <div>
      <h2>Docker</h2>
      <p class="sub">
        Containers on {device.display_name || device.hostname}
        {#if device.hardware?.docker_info?.version}· engine {device.hardware.docker_info.version}{/if}
        {#if device.hardware?.docker_info}· {device.hardware.docker_info.running}/{device.hardware.docker_info.total} running{/if}
      </p>
    </div>
    {#if device.hardware && device.hardware.docker_available === false}
      <span class="warn">Docker not detected on this device</span>
    {/if}
  </header>

  <DockerTab deviceId={device.id} {authHeaders} />
</section>

<style>
  .docker { display: grid; gap: 0.85rem; }
  .docker-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.75rem; flex-wrap: wrap; }
  .docker-head h2 { margin: 0; font-size: 1rem; color: var(--text-bright); }
  .sub { margin: 0.15rem 0 0; color: var(--text-muted); font-size: 0.76rem; }
  .warn { color: var(--warn); font-size: 0.72rem; font-weight: 600; }
</style>
