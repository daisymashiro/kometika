# 🤖 Bot Kometika

Bot Telegram multi-downloader pribadi (MTProto via `gotd/td`) untuk mengambil video/foto/audio dari berbagai platform dan meneruskannya ke chat Telegram — grup, channel, forum topic, dan private chat.

---

## ✨ Fitur Utama

### 📥 Downloader Platform

| Platform | Jalur | Keterangan |
|---|---|---|
| **TikTok** | `/dl <url>` / auto-detect | Video + audio, dukungan multi-reel |
| **Instagram** | `/dl <url>` / auto-detect | Video + foto, audio opsional |
| **Facebook** | `/dl <url>` / auto-detect | Video |
| **Twitter/X** | `/dl <url>` / auto-detect | Video (fxtwitter, dll) |
| **Douyin/RedNote** | `/dl <url>` / auto-detect | Scraper douyin + Xiaohongshu |
| **YouTube** | `/play`, `/music` | Play video, audio, live stream |
| **Terabox** | auto-detect / `/dl` | List file + menu tombol (download single/stream/zip) |
| **MediaFire** | auto-detect / `/dl` | File |
| **AceImg** | auto-detect / `/dl` | Hosting gambar |
| **Lulustream/LuluVid** | auto-detect / `/dl` | Video |
| **Droplink** | auto-detect | Buka shortlink ber-antivirus → link asli (hanya private chat) |

Deteksi link berjalan otomatis: kirim link apa pun ke bot/grup, tanpa command pun bot mengenali platform dan memproses.

### 🎮 Command

| Command | Fungsi |
|---|---|
| `/dl <url>` | Download manual dari URL |
| `/gdn <url>` | Download file Google Drive |
| `/play <url>` | Putar video YouTube |
| `/music <url>` | Download audio YouTube |
| `/fiture` / `/status` | List status semua fitur downloader |
| `/on <fitur>` / `/off <fitur>` | Aktif/nonaktifkan fitur (owner) |
| `/start` | Sambutan bot |
| `/help` | Bantuan |
| `/ping` | Cek bot hidup |
| `/uptime` | Uptime + info sistem (owner) |
| `/vnstat` | Monitoring bandwidth (owner) |
| `/speedtest` | Tes kecepatan server (owner) |
| `/getid`, `/groupinfo` | Info ID user/chat (owner) |
| `/botmode` | Ganti mode bot (owner) |
| `/goroutine` | Info goroutine (private, owner) |

### 🔒 Kontrol Fitur (On/Off + Database)

- Semua fitur downloader bisa dimatikan/dinyalakan owner via `/off <nama>` `/on <nama>`
- Status tersimpan di SQLite (`feature_toggles`) — tetap aktif setelah restart
- Default: semua fitur aktif

### 🏠 Batasan Privasi

- **Droplink**: hanya diproses di private chat (DM) — link di grup/channel diabaikan
- **Terabox**: mendukung private chat (menu penuh) + grup/channel

### 🌐 Sistem Proxy Download

Khusus kasus CDN memblokir/blokir regional:

- **Terabox**: download selalu lewat **proxy acak** dari `proxy_alive.txt`
  - Cache daftar proxy **3 jam** (sumber dicek ~2×/24 jam)
  - Dukungan **HTTP / SOCKS4 / SOCKS5** (dialer sendiri, tanpa deps tambahan)
  - **Retry 3 proxy berbeda** per unduhan; proxy mati → ganti otomatis
  - **Verifikasi ukuran file** vs metadata; ukuran berubah (proxy MITM) → dibuang, coba proxy lain
- **Instagram**: stream normal gagal → **fallback otomatis lewat proxy**
- Catatan keamanan: proxy publik jalan dengan `InsecureSkipVerify` (banyak yang MITM TLS) — wajar untuk share publik; lihat komentar kode.

---

## 🧱 Arsitektur

```
main.go                  — init, worker pool (150), job queue (1000), auto-detect link
internal/
├── api/                → semua API scraper + proxy (terabox, tiktok, ig, fb, youtube, shortener)
├── commands/           → handler bot + router command
├── cache/              → cache in-memory (proxy 3 jam, audio, live session, channel) + janitor
├── config/             → deteksi platform, fitur manager (on/off)
├── db/                 → SQLite (feature_toggles)
├── media/              → kirim media/stream ke Telegram, konversi gambar
├── ratelimit/          → rate limit
├── streamer/           → live-stream (mediamtx, muxer)
└── log/                → log ke Telegram (error callback)
```

Detail penting:
- **Cache TTL** dibersihkan janitor 10 menit (`internal/cache/janitor.go`) — audio 10', live 15', channel 24 jam
- Log file `mybot.log` **rotasi otomatis 50MB**
- Circuit breaker per API (gagal 3x → cooldown)

---

## 🚀 Deploy

### Prasyarat

- Go 1.2x
- Untuk link Droplink: **chromium playwright** sekali saja

### Env (`.env`)

```
TELEGRAM_API_ID=...
TELEGRAM_API_HASH=...
TELEGRAM_BOT_TOKEN=...
TELEGRAM_BOT_USERNAME=...

# Terabox: ndus cookie (akun verified)
TERABOX_NDUS=

# Owner
OWNER_ID=...            OWNER_ONLINE_WINDOW=300

# Opini (AI), RTMP, Firebase, dll — lihat .env.example
```

### Build & Run

```bash
go build -o kometika_bot .
./kometika_bot
```

### Droplink di VPS (wajib sekali)

```bash
# VPS tanpa Go? pakai skrip ini — install chromium + driver langsung dari internet
bash deploy/install-playwright.sh
```

Cukup sekali per VPS, setelah itu bot bisa buka link droplink (~20-45 detik, ModeFast).

---

## 🗄️ Data & Penyimpanan

| File | Isi |
|---|---|
| `session.json` | Session MTProto (hotd auto-managed) |
| `kometika_bot.db` | SQLite: `feature_toggles` |
| `mybot.log` + `mybot.log.1` | Log rotasi 50MB |
| `internal/assets/` | Thumbnail default, embed |

---

## ⚠️ Legal/Ethical

Bot untuk penggunaan pribadi. Hargai ToS masing-masing platform; gunakan dengan bijak.