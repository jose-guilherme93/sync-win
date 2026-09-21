# Preference Sync Format

This project stores a lightweight set of user preference files and game save data for each Linux device. The server acts as a preference archive and visibility dashboard, not a restoration engine.

## Format overview

```text
PreferenceSyncPayload
├── metadata
├── device
├── user
├── files
├── sync
└── capabilities
```

This is not a snapshot system. It is a sync-oriented preference structure intended for small text files and base64-encoded binary saves.

## Metadata

The `metadata` section contains:

- bundle_id
- device_id
- user_id
- created_at
- updated_at
- version
- source_agent_version

## Device

The `device` section contains:

- hostname
- os_name
- os_version
- architecture
- desktop_environment
- last_seen_at
- last_sync_at
- status

## User

The `user` section includes minimal identity attributes, such as:

- user_id
- username
- display_name

## Files

The `files` section contains a list of tracked preference files, each with:

- filename
- relative_path
- category
- content (text or base64-encoded binary)
- encoding ("base64" for binary saves, empty for text)
- content_hash (SHA-256)
- size_bytes
- synced_at
- status

Important: text files must be small (up to 256 KiB), inside explicit allowlists. Save-game files are allowed up to 1 MiB each as base64-encoded binary.

## Sync

The `sync` section contains:

- last_sync_at
- status
- last_error
- retry_count
- sync_version

This is the metadata used by the web UI to show the state of each device.

## Capabilities

The `capabilities` section describes supported preference categories:

- kde_preferences
- shell_preferences
- app_settings
- desktop_settings
- saves

These are lightweight declarations, not restore logic.

## Versioning

Preference bundles have a version field so the format can evolve without ambiguity. The version is incremented only when the payload structure or expected semantics change.

## IDs and hashing

Use a stable device ID and an explicit file hash. A SHA-256 hash is enough for content validation and deduplication of repeated text blocks. Content hashes enable the agent to skip re-uploading unchanged files.

## Storage rules

The server stores:

- small text files (up to 256 KiB)
- base64-encoded binary save files (up to 1 MiB)
- JSON metadata
- small per-device bundles
- minimal sync status records

The server does not store:

- full home directories
- large media files
- raw binary application states
- full environment snapshots

## Compatibility

Compatibility is not a restore compatibility problem. This system is about user preference sync and visibility. The app should validate that the files are still readable and safe to sync, but it is not responsible for complex environment migration.

## Integrity

Integrity is protected by:

- validation before upload (size, encoding, content)
- SHA-256 hashing on save
- content hash comparison to skip unchanged files
- rejection of dangerous or oversized content
- server-side deduplication by content hash

## Security constraints

Preference bundles must never include:

- passwords
- tokens
- private keys
- browser credentials
- KWallet data
- secret environment variables

Only explicitly safe text files should be allowed. Binary saves (base64) bypass secret scanning since content cannot be inspected.

## Design goal

The format exists to keep a simple, safe, and small record of user preferences and game saves per Linux device. The server is a lightweight preference archive, not a restoration engine.
