const DB_NAME = 'lem-telemetry'
const DB_VERSION = 1
const STORE_NAME = 'history'

// The cache key includes the exact from/to window, so every range the user
// tries creates a new entry. Without pruning, IndexedDB grows forever. Entries
// are kept for a day and the store is capped so a long-lived dashboard cannot
// accumulate unbounded history.
const MAX_ENTRY_AGE_MS = 24 * 60 * 60 * 1000
const MAX_ENTRIES = 300
const PRUNE_INTERVAL_MS = 5 * 60 * 1000

interface CacheEntry {
  key: string
  device_id: string
  resolution: string
  from: string
  to: string
  points: any[]
  bytes: number
  fetched_at: number
}

let dbPromise: Promise<IDBDatabase> | null = null

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        const store = db.createObjectStore(STORE_NAME, { keyPath: 'key' })
        store.createIndex('device_id', 'device_id', { unique: false })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => {
      // Do not cache a rejected promise forever: a transient IndexedDB failure
      // would otherwise disable the cache for the rest of the session.
      dbPromise = null
      reject(req.error)
    }
  })
  return dbPromise
}

function makeKey(deviceID: string, resolution: string, from: string, to: string): string {
  return `${deviceID}:${resolution}:${from}:${to}`
}

// estimateBytes measures what an entry actually costs. The old code reported
// count * 1024, a made-up number that had no relationship to the real payload.
function estimateBytes(points: any[]): number {
  try {
    return new Blob([JSON.stringify(points)]).size
  } catch {
    return 0
  }
}

let lastPrune = 0

// prune removes expired entries and, when the store is over the cap, the oldest
// ones beyond it. Runs at most once per PRUNE_INTERVAL_MS.
function schedulePrune(db: IDBDatabase) {
  if (Date.now() - lastPrune < PRUNE_INTERVAL_MS) return
  lastPrune = Date.now()
  void pruneStore(db)
}

function pruneStore(db: IDBDatabase): Promise<void> {
  return new Promise((resolve) => {
    let tx: IDBTransaction
    try {
      tx = db.transaction(STORE_NAME, 'readwrite')
    } catch {
      resolve()
      return
    }
    const store = tx.objectStore(STORE_NAME)
    const cutoff = Date.now() - MAX_ENTRY_AGE_MS
    const survivors: { key: string; fetchedAt: number }[] = []

    const req = store.openCursor()
    req.onsuccess = () => {
      const cursor = req.result
      if (!cursor) {
        // The cursor walks in key order, which starts with the device id, so it
        // is not age order. Trim the oldest by fetched_at when over the cap.
        if (survivors.length > MAX_ENTRIES) {
          survivors.sort((a, b) => a.fetchedAt - b.fetchedAt)
          for (const stale of survivors.slice(0, survivors.length - MAX_ENTRIES)) {
            try {
              store.delete(stale.key)
            } catch {
              // entry already removed
            }
          }
        }
        return
      }
      const entry = cursor.value as CacheEntry
      if (entry.fetched_at < cutoff) {
        cursor.delete()
      } else {
        survivors.push({ key: entry.key, fetchedAt: entry.fetched_at })
      }
      cursor.continue()
    }
    req.onerror = () => resolve()
    tx.oncomplete = () => resolve()
    tx.onerror = () => resolve()
    tx.onabort = () => resolve()
  })
}

export async function getCachedHistory(
  deviceID: string,
  resolution: string,
  from: string,
  to: string,
  maxAgeMs: number = 60_000
): Promise<any[] | null> {
  try {
    const db = await openDB()
    const key = makeKey(deviceID, resolution, from, to)
    return await new Promise<any[] | null>((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readonly')
      const store = tx.objectStore(STORE_NAME)
      const req = store.get(key)
      req.onsuccess = () => {
        const entry: CacheEntry | undefined = req.result
        if (entry && Date.now() - entry.fetched_at < maxAgeMs) {
          resolve(entry.points)
        } else {
          resolve(null)
        }
      }
      req.onerror = () => resolve(null)
    })
  } catch {
    return null
  }
}

export async function setCachedHistory(
  deviceID: string,
  resolution: string,
  from: string,
  to: string,
  points: any[]
): Promise<void> {
  try {
    const db = await openDB()
    const entry: CacheEntry = {
      key: makeKey(deviceID, resolution, from, to),
      device_id: deviceID,
      resolution,
      from,
      to,
      points,
      bytes: estimateBytes(points),
      fetched_at: Date.now()
    }
    await new Promise<void>((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readwrite')
      tx.objectStore(STORE_NAME).put(entry)
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
      tx.onabort = () => resolve()
    })
    schedulePrune(db)
  } catch {
    // Cache errors are non-fatal
  }
}

export async function clearCache(deviceID?: string): Promise<void> {
  try {
    const db = await openDB()
    await new Promise<void>((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readwrite')
      const store = tx.objectStore(STORE_NAME)
      if (deviceID) {
        const idx = store.index('device_id')
        const req = idx.openCursor(IDBKeyRange.only(deviceID))
        req.onsuccess = () => {
          const cursor = req.result
          if (cursor) {
            cursor.delete()
            cursor.continue()
          }
        }
      } else {
        store.clear()
      }
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
    })
  } catch {
    // Cache errors are non-fatal
  }
}

// getCacheStats reports the real serialised size of what is stored. The
// previous count * 1024 was a fabricated figure with no relation to the payload.
export async function getCacheStats(): Promise<{ count: number; sizeEstimate: number }> {
  try {
    const db = await openDB()
    return await new Promise((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readonly')
      const store = tx.objectStore(STORE_NAME)
      let count = 0
      let bytes = 0
      const req = store.openCursor()
      req.onsuccess = () => {
        const cursor = req.result
        if (!cursor) {
          resolve({ count, sizeEstimate: bytes })
          return
        }
        const entry = cursor.value as CacheEntry
        count++
        bytes += entry.bytes || estimateBytes(entry.points)
        cursor.continue()
      }
      req.onerror = () => resolve({ count, sizeEstimate: bytes })
    })
  } catch {
    return { count: 0, sizeEstimate: 0 }
  }
}
