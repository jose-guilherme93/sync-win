<script lang="ts">
  import DockerTab from '../DockerTab.svelte'
  import type { Device } from '../../lib/types'

  // Docker is a first-class screen. The page title and the device name are in
  // the shell, so this wrapper only hands the panel what it cannot fetch
  // itself: the hardware sample, for an engine line on the first paint.
  export let device: Device
  export let authHeaders: Record<string, string> = {}
</script>

<DockerTab deviceId={device.id} {authHeaders} hardware={device.hardware} />
