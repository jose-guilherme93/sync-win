// Local OpenCode V2 adapter for Ponytail 4.10.0.
// Upstream still default-exports the V1 plugin function, which OpenCode V2 rejects.

import fs from "fs"
import os from "os"
import path from "path"
import { fileURLToPath } from "url"

const MODES = new Set(["off", "lite", "full", "ultra", "review"])
const DEFAULT_MODE = "full"
const MODE_KEY = "mode"
const SKILL_PATH = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../skills/ponytail/SKILL.md")

function normalizeMode(value) {
  if (typeof value !== "string") return undefined
  const mode = value.trim().toLowerCase()
  return MODES.has(mode) ? mode : undefined
}

function defaultMode() {
  const fromEnv = normalizeMode(process.env.PONYTAIL_DEFAULT_MODE)
  if (fromEnv) return fromEnv

  try {
    const configHome = process.env.XDG_CONFIG_HOME || path.join(os.homedir(), ".config")
    const config = JSON.parse(fs.readFileSync(path.join(configHome, "ponytail", "config.json"), "utf8").replace(/^\uFEFF/, ""))
    return normalizeMode(config.defaultMode) || DEFAULT_MODE
  } catch {
    return DEFAULT_MODE
  }
}

function loadSkillBody() {
  try {
    return fs
      .readFileSync(SKILL_PATH, "utf8")
      .replace(/^---[\s\S]*?---\s*/, "")
      .trim()
  } catch {
    return "You are a lazy senior developer. Prefer reuse, the standard library, native platform features, and the minimum code that works. Never trade away input validation, error handling, security, or accessibility."
  }
}

const SKILL_BODY = loadSkillBody()

function instructionsFor(mode) {
  const effectiveMode = normalizeMode(mode) || DEFAULT_MODE
  if (effectiveMode === "review") {
    return `PONYTAIL MODE ACTIVE — level: review. Behavior defined by the ponytail-review skill.`
  }

  const body = SKILL_BODY.split(/\r?\n/)
    .filter((line) => {
      const table = line.match(/^\|\s*\*\*(.+?)\*\*\s*\|/)
      if (table) {
        const level = normalizeMode(table[1])
        return !level || level === effectiveMode
      }

      const example = line.match(/^-\s*([^:]+):\s*"/)
      if (example) {
        const level = normalizeMode(example[1])
        return !level || level === effectiveMode
      }

      return true
    })
    .join("\n")

  return `PONYTAIL MODE ACTIVE — level: ${effectiveMode}\n\n${body}`
}

export default {
  id: "ponytail",
  async setup(ctx) {
    await ctx.command.transform((editor) => {
      editor.add({
        name: "ponytail",
        description: "Switch ponytail intensity level (lite/full/ultra/off)",
        async execute({ sessionID, prompt, delivery }) {
          const requested = String(prompt?.text || "").trim()
          const mode = normalizeMode(requested || defaultMode())

          if (!mode) {
            await ctx.session.prompt({
              ...prompt,
              sessionID,
              delivery,
              text: `Invalid Ponytail level "${requested}". Use lite, full, ultra, or off.`,
            })
            return
          }

          await ctx.storage.set(MODE_KEY, mode)
          await ctx.session.prompt({
            ...prompt,
            sessionID,
            delivery,
            text: `Ponytail mode set to ${mode}. Apply that level until it is changed again.`,
          })
        },
      })
    })

    await ctx.session.hook("context", async (event) => {
      const stored = await ctx.storage.get(MODE_KEY)
      const mode = normalizeMode(stored) || defaultMode()
      if (mode === "off") return
      event.system.push({ type: "text", text: instructionsFor(mode) })
    })
  },
}
