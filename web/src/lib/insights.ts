import { memoryPercent, type HardwareStats } from './types'

// Fleet-wide health insights.
//
// This logic used to live inside App.svelte, which meant no other view could
// surface the same alert. Moving it into lib/ lets the Home dashboard, the
// device cards and the future Alerts screen agree on what "needs attention"
// means.

export type InsightType = 'info' | 'warn' | 'crit'
export type Insight = { text: string; type: InsightType }

// Thresholds are duplicated from the meter colours on purpose: these are the
// points at which the UI changes state, and keeping them in one list is what
// stops the card turning amber at 75% while the alert fires at 80%.
export const THRESHOLDS = {
  cpuWarn: 70,
  cpuCrit: 90,
  ramWarn: 75,
  ramCrit: 90,
  tempWarn: 70,
  tempCrit: 85,
  diskWarn: 80,
  diskCrit: 90,
  agentCpuWarn: 5,
  batteryLow: 15
} as const

export function generateInsights(h?: HardwareStats): Insight[] {
  if (!h) return []
  const insights: Insight[] = []
  const cpu = h.cpu_usage_percent || 0
  const mem = memoryPercent(h)
  const agentCpu = h.agent_cpu_usage || 0

  if (cpu > THRESHOLDS.cpuCrit) insights.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'crit' })
  else if (cpu > THRESHOLDS.cpuWarn) insights.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'warn' })

  if (mem > THRESHOLDS.ramCrit) insights.push({ text: `RAM ${mem.toFixed(0)}%`, type: 'crit' })
  else if (mem > THRESHOLDS.ramWarn) insights.push({ text: `RAM ${mem.toFixed(0)}%`, type: 'warn' })

  if (h.cpu_temperature && h.cpu_temperature > THRESHOLDS.tempCrit) {
    insights.push({ text: `Temp ${h.cpu_temperature.toFixed(0)}°C`, type: 'crit' })
  } else if (h.cpu_temperature && h.cpu_temperature > THRESHOLDS.tempWarn) {
    insights.push({ text: `Temp ${h.cpu_temperature.toFixed(0)}°C`, type: 'warn' })
  }

  if (h.battery_percent != null && h.battery_percent > 0 && h.battery_percent < THRESHOLDS.batteryLow && h.battery_status !== 'charging') {
    insights.push({ text: `Battery ${h.battery_percent.toFixed(0)}%`, type: 'crit' })
  }

  for (const part of h.disk_partitions ?? []) {
    if (part.used_percent > THRESHOLDS.diskCrit) {
      insights.push({ text: `${part.mount} ${part.used_percent.toFixed(0)}% full`, type: 'crit' })
    } else if (part.used_percent > THRESHOLDS.diskWarn) {
      insights.push({ text: `${part.mount} ${part.used_percent.toFixed(0)}%`, type: 'warn' })
    }
  }

  for (const iface of h.network_ifaces ?? []) {
    if (iface.rx_errors > 0 || iface.tx_errors > 0) {
      insights.push({ text: `Net err ${iface.name}`, type: 'warn' })
    }
  }

  if (agentCpu > THRESHOLDS.agentCpuWarn) {
    insights.push({ text: `Agent ${agentCpu.toFixed(1)}%`, type: 'warn' })
  }

  if (h.kernel_version) {
    const parts = h.kernel_version.split('.')
    if (parts.length >= 2) {
      const major = parseInt(parts[0], 10)
      const minor = parseInt(parts[1], 10)
      if (major < 5 || (major === 5 && minor < 15)) {
        insights.push({ text: `Kernel ${h.kernel_version}`, type: 'warn' })
      }
    }
  }

  return insights
}
