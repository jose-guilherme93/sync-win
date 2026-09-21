const DB_NAME = 'lem-telemetry'
const DB_VERSION = 1
const STORE_NAME = 'history'

interface CacheEntry {
  key: string
  device_id: string
  resolution: string
  from: string
  to: string
  points: any[]
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
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

function makeKey(deviceID: string, resolution: string, from: string, to: string): string {
  return `${deviceID}:${resolution}:${from}:${to}`
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
      fetched_at: Date.now()
    }
    await new Promise<void>((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readwrite')
      tx.objectStore(STORE_NAME).put(entry)
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
    })
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

export async function getCacheStats(): Promise<{ count: number; sizeEstimate: number }> {
  try {
    const db = await openDB()
    return await new Promise((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readonly')
      const store = tx.objectStore(STORE_NAME)
      const req = store.count()
      req.onsuccess = () => resolve({ count: req.result, sizeEstimate: req.result * 1024 })
      req.onerror = () => resolve({ count: 0, sizeEstimate: 0 })
    })
  } catch {
    return { count: 0, sizeEstimate: 0 }
  }
}
