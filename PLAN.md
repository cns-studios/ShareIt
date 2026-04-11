# ShareIt Implementation Plan: True E2E Device Keys + Recently Uploaded

## 1) Objective

Implement authenticated "Recently Uploaded" history for CNS users while preserving true end-to-end encryption (E2E):

- Server must never be able to decrypt file content.
- Logged-in users should see their own uploads in a list under the upload zone.
- Users should be able to download and decrypt files directly from that list.
- New devices should gain access through trusted device approval (option 1), not server-side key escrow.

This document is written for an engineer unfamiliar with this codebase and is intended as an implementation-ready guide.

## 2) Current Codebase Baseline

What exists today:

- Upload flow:
  - Client encrypts file in browser with a generated passphrase and uploads encrypted blob in chunks.
  - Upload APIs: `/api/upload/init`, `/api/upload/chunk`, `/api/upload/complete`, `/api/upload/finalize`.
  - Final share URL is `.../shared/{fileId}#{passphrase}`.
- Download flow:
  - Server returns encrypted blob bytes. Browser decrypts using key from URL hash.
- Auth:
  - Optional CNS auth middleware sets `cns_user` in request context if `auth_token` is valid.
- Database:
  - `files` table has no CNS user ownership fields.
  - No key envelope tables yet.

Implication:

- Current model is still E2E for shared links, but there is no authenticated per-user key persistence or trusted-device onboarding.

## 3) Security Model and Non-Negotiable Rules

### 3.1 Threat model

Assume attacker may obtain:

- PostgreSQL data dump
- Redis dump
- application server filesystem

Security target:

- Attacker cannot decrypt stored files without client-held secrets.

### 3.2 Rules

1. File Data Encryption Key (DEK) is random per file.
2. DEK is generated client-side only.
3. Server stores only wrapped/encrypted DEKs (key envelopes), never plaintext DEKs.
4. Unwrapping keys happens only client-side on authorized device.
5. New device enrollment requires approval from an already authorized device.
6. If user has no authorized device and no recovery mechanism, old files are unrecoverable by design.

## 4) Target Cryptographic Design (Option 1: Live Device Approval)

Use a hybrid key hierarchy:

- `DEK` (random, per file) encrypts file bytes.
- `UK` (User Key, symmetric root key) wraps each DEK.
- Each device has asymmetric keypair (`DevicePublicKey`, `DevicePrivateKey`) generated in browser.
- `UK` is wrapped to every authorized device public key.

Data at rest on server:

- encrypted file bytes
- `encrypted_dek_for_file` (DEK wrapped by UK)
- `encrypted_uk_for_device` per device (UK wrapped by device public key)
- metadata (non-secret)

Server cannot decrypt because it has:

- no plaintext UK
- no device private keys
- no plaintext DEKs

## 5) New Product Behavior

### 5.1 Upload behavior (logged in)

1. Browser ensures a device keypair exists for current device.
2. Browser obtains UK (already unwrapped locally) or initializes UK on first trusted device.
3. Browser creates random DEK for file.
4. Browser encrypts file with DEK.
5. Browser wraps DEK with UK.
6. Upload ciphertext using existing chunk flow.
7. Finalize upload and store:
   - owner CNS user id
   - wrapped DEK envelope
   - algorithm/version metadata
8. Recently uploaded list shows the item immediately.

### 5.2 Recently uploaded list behavior

- Visible only for authenticated CNS users.
- Sorted newest to oldest.
- Each row: filename, file size, upload date, expiration date, download button, copy sharing link button.
- Date shorteners:
  - Upload date: `Today`, `Yesterday`, else local short date.
  - Expiration date: `Tomorrow` if tomorrow, `HH:MM` if today, else local short date.

### 5.3 Download from list

1. Browser requests file metadata and wrapped DEK for that owned file.
2. Browser unwraps UK using local device private key.
3. Browser unwraps DEK using UK.
4. Browser downloads encrypted blob and decrypts locally.
5. Browser saves/open file.

### 5.4 New device enrollment

1. New device logs in with CNS and generates device keypair.
2. New device creates enrollment request + short code.
3. Existing trusted device lists pending enrollments.
4. User confirms code on trusted device.
5. Trusted device re-wraps UK to new device public key and submits approval.
6. New device fetches wrapped UK and becomes trusted.

No server-side decryption occurs at any point.

## 6) Required Database Migrations

Create folder and files:

- `/db/migrations/001-add-user-device-and-file-owner.sql`
- `/db/migrations/002-add-key-envelopes.sql`
- `/db/migrations/003-add-device-enrollment.sql`

### 6.1 Migration 001: ownership + devices

Changes:

1. Add owner fields to `files`:
   - `owner_cns_user_id BIGINT NULL`
   - `owner_cns_username TEXT NULL`
2. Create `user_devices`:
   - `id UUID PK`
   - `cns_user_id BIGINT NOT NULL`
   - `device_label TEXT NULL`
   - `public_key_jwk JSONB NOT NULL` (or SPKI base64 in TEXT)
   - `key_algorithm TEXT NOT NULL`
   - `key_version INT NOT NULL DEFAULT 1`
   - `created_at`, `last_seen_at`, `revoked_at`
3. Indexes:
   - `files(owner_cns_user_id, created_at DESC)`
   - `user_devices(cns_user_id, revoked_at)`

### 6.2 Migration 002: envelopes

Create `file_key_envelopes`:

- `file_id VARCHAR(20) PK references files(id) on delete cascade`
- `wrapped_dek BYTEA NOT NULL`
- `dek_wrap_alg TEXT NOT NULL` (example: `AES-KW-UK-v1`)
- `dek_wrap_nonce BYTEA NULL` (if algorithm requires nonce)
- `dek_wrap_version INT NOT NULL DEFAULT 1`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`

Create `user_key_envelopes`:

- `id UUID PK`
- `cns_user_id BIGINT NOT NULL`
- `device_id UUID NOT NULL references user_devices(id) on delete cascade`
- `wrapped_user_key BYTEA NOT NULL`
- `uk_wrap_alg TEXT NOT NULL` (example: `ECDH-ES+A256KW-v1`)
- `uk_wrap_meta JSONB NOT NULL` (ephemeral pubkey, nonce, etc)
- `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- unique index `(cns_user_id, device_id)`

### 6.3 Migration 003: enrollment

Create `device_enrollments`:

- `id UUID PK`
- `cns_user_id BIGINT NOT NULL`
- `request_device_id UUID NOT NULL references user_devices(id) on delete cascade`
- `verification_code VARCHAR(16) NOT NULL`
- `status TEXT NOT NULL` (`pending`, `approved`, `rejected`, `expired`)
- `approved_by_device_id UUID NULL references user_devices(id)`
- `expires_at TIMESTAMPTZ NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`

Indexes:

- `(cns_user_id, status, created_at DESC)`
- partial unique index to limit one active pending enrollment per requesting device.

## 7) Backend Implementation Plan (Go)

### 7.1 Models

Extend `internal/models/models.go` with:

- `OwnedFileListItem`
- `FileKeyEnvelope`
- `UserDevice`
- `UserKeyEnvelope`
- `DeviceEnrollment` structs
- request/response DTOs for new APIs below

### 7.2 Storage layer

Add methods in `internal/storage/postgres.go`:

- `CreateOrUpdateUserDevice(...)`
- `GetActiveDevicesByUser(...)`
- `CreateFileWithOwner(...)` or extend existing `CreateFile` with owner fields
- `SaveFileKeyEnvelope(...)`
- `GetOwnedRecentFiles(...)`
- `GetOwnedFileWithEnvelope(...)`
- enrollment CRUD:
  - `CreateEnrollmentRequest(...)`
  - `ListPendingEnrollments(...)`
  - `ApproveEnrollment(...)`
  - `GetEnrollmentByID(...)`
- `SaveUserKeyEnvelope(...)`
- `GetUserKeyEnvelopeForDevice(...)`

### 7.3 Upload service

Update upload finalization path in `internal/services/upload.go`:

- Accept optional owner info (from CNS middleware context, passed through handler).
- For authenticated uploads, persist `owner_cns_user_id` and `owner_cns_username`.
- Persist file key envelope transactionally with file row.

Do not change guest upload behavior.

### 7.4 Handlers and routes

Add new handler file, for example `internal/handlers/recent_uploads.go`:

- `GET /api/me/recent-uploads`
  - auth required (CNS user in context)
  - returns list sorted by `created_at DESC`
- `GET /api/me/files/:id/access`
  - verifies ownership
  - returns file metadata + wrapped DEK envelope
- `POST /api/me/devices/register`
  - registers/refreshes device public key
- `POST /api/me/devices/enrollments`
  - creates enrollment request from new device
- `GET /api/me/devices/enrollments/pending`
  - list pending requests for trusted devices
- `POST /api/me/devices/enrollments/:id/approve`
  - trusted device submits UK wrapped for requesting device

Wire routes in `cmd/server/main.go` under existing `/api` group.

### 7.5 Authorization checks

For every new endpoint:

- Reject if not authenticated.
- Ensure `files.owner_cns_user_id == cns_user.id` before returning envelopes.
- Ensure only same user can create/approve enrollments.

### 7.6 Validation and abuse controls

- Reuse CSRF middleware for browser calls.
- Add lightweight rate limit for enrollment creation/approval.
- Enrollment codes expire quickly (example: 10 minutes).

## 8) Frontend Implementation Plan

### 8.1 Crypto module changes

Current `web/static/js/crypto.js` is passphrase-based for share links.

Add capabilities without removing current share-link behavior:

- Device keypair generation and persistence (IndexedDB preferred).
- UK generation (once) and local cache in memory/session.
- Wrap/unwrap helpers:
  - wrap DEK with UK
  - unwrap DEK with UK
  - wrap UK with device public key
  - unwrap UK with device private key

### 8.2 Upload app flow changes

Update `web/static/js/app.js` upload finalize flow for authenticated users:

- After encrypting file, produce `wrappedDEK` payload.
- Send `wrappedDEK` + metadata in finalize request for authenticated uploads.
- Keep existing public share URL + fragment behavior for interoperability.

### 8.3 Recently uploaded UI

In `web/templates/index.html`:

- Add authenticated-only section under upload zone:
  - title: "Recently Uploaded"
  - list container with loading, empty, and error states

In `web/static/js/app.js`:

- On page init, if authenticated, fetch `/api/me/recent-uploads`.
- Render rows sorted by `created_at DESC`.
- Row actions:
  - Download: call access endpoint, unwrap keys, download/decrypt.
  - Copy link: copy share link including fragment key if available locally.

### 8.4 Date and size formatting rules

Implement helpers:

- `formatUploadDate(date)`:
  - today -> `Today HH:MM`
  - yesterday -> `Yesterday HH:MM`
  - else local short date/time
- `formatExpiryDate(date)`:
  - expired -> `Expired`
  - today -> `HH:MM`
  - tomorrow -> `Tomorrow`
  - else local short date

Use existing `SecureCrypto.formatFileSize` for size display consistency.

## 9) API Contract Draft

### 9.1 `GET /api/me/recent-uploads`

Response:

```json
{
  "items": [
    {
      "file_id": "abc123...",
      "filename": "report.pdf",
      "size_bytes": 123456,
      "created_at": "2026-04-11T09:00:00Z",
      "expires_at": "2026-04-18T09:00:00Z",
      "share_url": "https://host/shared/abc123..."
    }
  ]
}
```

### 9.2 `GET /api/me/files/:id/access`

Response:

```json
{
  "file": {
    "id": "abc123...",
    "original_name": "report.pdf",
    "size_bytes": 123456,
    "created_at": "...",
    "expires_at": "..."
  },
  "file_key_envelope": {
    "wrapped_dek_b64": "...",
    "dek_wrap_alg": "AES-KW-UK-v1",
    "dek_wrap_nonce_b64": "...",
    "dek_wrap_version": 1
  },
  "user_key_envelope": {
    "wrapped_uk_b64": "...",
    "uk_wrap_alg": "ECDH-ES+A256KW-v1",
    "uk_wrap_meta": {"epk": "...", "nonce": "..."},
    "key_version": 1
  }
}
```

### 9.3 `POST /api/me/devices/register`

Request:

```json
{
  "device_id": "uuid",
  "device_label": "Aaron Laptop",
  "public_key_jwk": {"kty":"EC", "crv":"P-256", "x":"...", "y":"..."},
  "key_algorithm": "ECDH-P256"
}
```

### 9.4 Enrollment request/approve

- `POST /api/me/devices/enrollments` returns enrollment id + short code + expires_at.
- `POST /api/me/devices/enrollments/:id/approve` accepts wrapped UK for request device.

## 10) Rollout Strategy

### Phase A: schema + backend primitives

- Add migrations and storage methods.
- Add ownership writing during finalize.
- Add recent uploads read endpoint (without key unwrap usage first).

### Phase B: key envelopes + authenticated download path

- Store wrapped DEK at finalize for authenticated uploads.
- Add access endpoint returning envelopes.
- Add frontend decrypt-from-list flow.

### Phase C: device enrollment

- Add device register API.
- Add enrollment create/list/approve APIs.
- Add minimal UI dialogs for code confirmation.

### Phase D: hardening

- Rate limits, audits, revoked device handling, tests.

## 11) Test Plan

### 11.1 Unit tests

- Date shortener helpers (today/yesterday/tomorrow/expired).
- Ownership query filters.
- Enrollment state transitions.

### 11.2 Integration tests

1. Authenticated upload creates `files.owner_cns_user_id` and file envelope row.
2. `GET /api/me/recent-uploads` returns only user-owned files sorted newest first.
3. Access endpoint denies non-owner.
4. Device enrollment approval creates `user_key_envelope` for new device.

### 11.3 Manual E2E tests

1. Device A upload, appears in recent list, download works.
2. Device B login, request enrollment, approve from A, B can decrypt old file.
3. Database dump alone cannot decrypt file (no private keys).

## 12) Operational Notes

- Backward compatibility:
  - Existing links with `#passphrase` must continue to work.
  - Guest uploads remain unchanged.
- Logging:
  - Never log plaintext keys, wrapped blobs in full, or URL hash fragments.
- Recovery:
  - Option 1 intentionally has no server recovery. If all trusted devices are lost, encrypted files are not recoverable.

## 13) Acceptance Criteria

Feature is complete when all are true:

1. Authenticated users see "Recently Uploaded" list under upload zone.
2. List contains filename, file size, upload date, expiration date, download button, copy-sharing-link button.
3. List sorted newest to oldest.
4. Uploads are bound to authenticated CNS user in DB.
5. Keys are stored server-side only as encrypted envelopes.
6. Server cannot decrypt files using stored DB and filesystem only.
7. New device can decrypt old files only after trusted-device approval.

## 14) File-Level Work Checklist

- `db/migrations/001-add-user-device-and-file-owner.sql`
- `db/migrations/002-add-key-envelopes.sql`
- `db/migrations/003-add-device-enrollment.sql`
- `internal/models/models.go`
- `internal/storage/postgres.go`
- `internal/services/upload.go`
- `internal/handlers/upload.go`
- `internal/handlers/pages.go` (if extra config needed)
- `internal/handlers/recent_uploads.go` (new)
- `cmd/server/main.go` (route wiring)
- `web/templates/index.html`
- `web/static/js/crypto.js`
- `web/static/js/app.js`

This plan intentionally keeps the existing public sharing UX while adding authenticated ownership and true E2E multi-device access through trusted device approval.