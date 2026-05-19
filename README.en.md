<p align="right">
   <a href="./README.md">中文</a> | <strong>English</strong>
</p>

<p align="center">
   <picture>
   <img style="width: 80%" src="https://pic1.imgdb.cn/item/6846e33158cb8da5c83eb1eb.png" alt="image__3_-removebg-preview.png"> 
    </picture>
</p>

<div align="center">

_This project is a secondary development based on [one-hub](https://github.com/MartialBE/one-api)_

<a href="https://t.me/+LGKwlC_xa-E5ZDk9">
  <img src="https://img.shields.io/badge/Telegram-AI Wave Community-0088cc?style=for-the-badge&logo=telegram&logoColor=white" alt="Telegram Group" />
</a>

<sup><i>AI Wave Community</i></sup> · <sup><i>(Offering public API and AI bots in-group)</i></sup>

### [📚 View Original Project Documentation](https://one-hub-doc.vercel.app/)

</div>

---

## About

**Done Hub** is a fork / secondary development of [one-hub](https://github.com/MartialBE/one-api). It stays **fully database- and image-compatible** with the upstream while adding substantial improvements around **reverse-proxying modern clients (Claude Code / Gemini CLI / Codex / Antigravity, etc.)**, **fine-grained channel control**, an **invitation & rebate system**, **analytics**, and **multi-instance deployment stability**.

> The database schema is compatible with the upstream — existing one-hub users can migrate by simply pulling the `deanxv/done-hub` image.

---

## Differences from Upstream (Latest one-hub Image)

The additions are grouped into ten categories: **new providers**, **native-route compatibility**, **channel-level enhancements**, **invitation & rebates**, **batch management**, **analytics & logging**, **auth**, **UI/UX**, **bug fixes**, and **performance / reliability**.

### 1. New Provider / Channel Types

| Channel | Description |
| --- | --- |
| **Codex** (reverse proxy) | New provider that reverse-proxies Codex CLI / Codex API as a standard upstream channel. |
| **Claude Code** (reverse proxy) | New ClaudeCode provider — terminates the Claude Code client (including the native `/v1/messages` protocol). |
| **Gemini CLI** (reverse proxy) | New GeminiCli channel (type `57`) for transparently relaying native Gemini CLI requests. |
| **Antigravity** (reverse proxy) | New Antigravity channel (type `60`) targeting alternative endpoints such as `daily-cloudcode-pa.googleapis.com`. |
| **Vertex AI Express** | New Vertex AI Express channel (type `61`) — a lighter-weight Vertex AI integration. |

### 2. Native-Route Compatibility Across Channels

Upstream providers can normally only be called via their own protocol. Done Hub bridges different native protocols so that a single channel can simultaneously serve multiple client types:

- **Custom channels** can expose the **native Claude route** (`/v1/messages`) — ready to plug into Claude Code.
- **Vertex AI** channels can expose the **native Gemini route** (`/gemini/*`) — ready to plug into Gemini CLI.
- **Vertex AI** channels can expose the **native Claude route** — ready to plug into Claude Code.
- **Vertex AI** channels support **multiple regions**; one region is randomly chosen per request, giving free per-request load balancing.
- **Google Gemini** channels support native **video generation** (Veo series) via the `/gemini` route.
- `gemini-2.0-flash-preview-image-generation` is supported for **text-to-image / image-to-image**, and is also compatible with the **OpenAI Chat Completions** interface.

### 3. Channel-Level Enhancements

- **Request parameter pass-through / filtering**
    - Native `/gemini` image-generation requests support **extra-parameter pass-through**.
    - Claude channels (both OpenAI format and native format) support **extra-parameter pass-through**.
    - `remove_params` supports **nested fields**, e.g. `"remove_params": ["generationConfig.thinkingConfig"]`.
- **Model name mapping**
    - Configurable **case-insensitive model name** matching.
    - Configurable **unified request / response model name** mapping.
    - Model redirection supports **auto-mapping the key** to model configuration.
- **BaseURL templating**
    - Channel `BaseURL` supports **model-variable substitution** for dynamic upstream URLs based on the request model.
- **Per-channel toggles**
    - **Bill empty responses** toggle (default: billed).
    - **Built-in chat** feature toggle.
    - Per-channel `allow_extra_body` to allow extra request body fields.

### 4. Invitation & Rebate System (new)

- **Invite Code module**
    - Manual / automatic code generation, **batch create** (up to 100 at a time), and **batch delete**.
    - Per-code **max uses**, **start / expiry timestamps**, and **enable / disable** flag.
    - Tracks usage and status statistics.
- **Recharge rebate for inviters**
    - When an invitee recharges, the inviter can be rewarded:
        - **Fixed amount** or **percentage** types.
    - Coexists with the original "invite bonus quota" feature, forming a full referral incentive loop.
- **Decouple third-party login from invite-code authorization**
    - Fixes the coupling that forced invite codes on third-party logins; the open-registration policy can now be configured independently.

### 5. Batch Management

- **Batch delete channels**
- **Batch add models** to multiple channels at once
- **Batch attach user groups** to multiple channels
- Batch operations are also available for invite codes, system logs, etc.

### 6. Analytics & Logging

- **Live RPM / TPM / CPM metrics** in the analytics dashboard.
- **Recharge statistics** now supports **time-period filters** (all / year / month / week / day).
- The system log (`system_log`) supports conditional queries and pagination.
- Logs can be **exported as CSV** for offline analysis and reconciliation.
- Removed the meaningless "original price" styling from log views.

### 7. Auth & Login

- **LinuxDo OAuth** login with **Trust Level**–based access control (Basic / Member / Regular / Leader).
- Third-party login flow decoupled from invite-code authorization.

### 8. UI / UX

- **System Information** module rebuilt — denser, more readable.
- **Dark mode follows the OS** automatically.
- Improved **email validation rules**.
- Many backend UI polish items (table sorting, icons, forms, hints, etc.).
- Better **disabled-channel email notification** logic to avoid duplicates / false positives.

### 9. Bug Fixes

Covers billing, caching, statistics, payments, security — the critical paths:

- Fixed broken user-facing API endpoints.
- Fixed missing fields in invitation records.
- Fixed **hardcoded timezone** affecting statistical data (now respects `TZ` / `time.Local`).
- Fixed **channels not reloading into memory** after updates.
- Fixed **payment-callback failures in multi-instance deployments**.
- Fixed **floating-point token math** for Zhipu **GLM** models.
- Fixed **CDN-caching on API routes** that led to privilege escalation.
- Fixed inconsistent **time-type formatting across MySQL versions**.
- Fixed multiple **user-quota cache / DB inconsistencies** that caused billing anomalies.
- Fixed missing **`cached_tokens`** field in the Responses API.
- Fixed parsing of **Anthropic-style cache tokens** in OpenAI-compatible `usage`.
- Fixed context propagation in the **Gemini CLI provider** during token refresh / error handling.

### 10. Performance & Reliability

- **Vertex AI auth caching** — significantly fewer redundant auth round-trips.
- **Key cache pre-warming** — higher cache hit rate at startup.
- **Channel cache reloads on update** — no restart required.
- Improved **retry / cooldown** logic for more stable failover.
- Cleaner **`google_search` responses** for `/gemini` requests.
- Unified and trimmed **log formatting** across the codebase for faster triage.
- Alipay payment pipeline refactor; Epay and related fixes.

---

## Deployment

> Follow the upstream deployment guide and swap the image for `deanxv/done-hub`.

> Database-compatible — existing one-hub deployments can migrate by simply pulling this image.

Quick start:

```bash
docker pull deanxv/done-hub:latest

docker run -d \
  --name done-hub \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data:/data \
  deanxv/done-hub:latest
```

> Full deployment, configuration, billing, and model-pricing docs live in the upstream documentation: <https://one-hub-doc.vercel.app/>

---

## Feedback & Community

- Telegram: [AI Wave Community](https://t.me/+LGKwlC_xa-E5ZDk9) (public API and AI bots available in-group)
- Issues / PRs are welcome on this repository.

## Acknowledgments

- This program builds on the following open-source project:
    - [one-hub](https://github.com/MartialBE/one-api) — the foundation of this project.

Thanks to the authors and contributors of the upstream project, and to everyone who has filed PRs, issues, or feedback for Done Hub.
