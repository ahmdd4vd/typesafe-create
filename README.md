<p align="center">
  <img src="assets/logo.png" alt="typesafe-create logo" width="420" />
</p>

<h1 align="center">typesafe-create</h1>

<p align="center">
  Auto-register akun <a href="https://typesafe.ai">typesafe.ai</a> + auto-bikin API key.<br/>
  Ditulis dalam Go — ringan, cepat, tanpa dependency eksternal.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/mail.tm-free%20API-7c3aed?style=flat-square" alt="mail.tm" />
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License" />
</p>

<p align="center">Email sementara dari <a href="https://mail.tm">mail.tm</a> (gratis, no API key).</p>

---

## Syarat

- [Go](https://go.dev/dl/) 1.22 atau lebih baru
- Proxy (opsional, tapi **sangat disarankan**)

---

## Cara Pakai

### 1. Isi proxy

Buka `proxies.txt`, isi satu proxy per baris:

```
http://USER:PASS@HOST:PORT
```

Baris diawali `#` akan diabaikan.

> **Klaim proxy gratis di sini:**
> **https://dashboard.proxyscrape.com/**

### 2. Jalankan

```bash
cd typesafe-create
go run .
```

Atau build dulu biar bisa di-share:

```bash
go build -o typesafe-create.exe .
./typesafe-create.exe
```

Contoh run lokal (10 akun, 8 worker):

```bash
./typesafe-create.exe -n 10 -c 8
```

### Flag

| Flag | Default | Fungsi |
|------|---------|--------|
| `-n` | `612` | Jumlah akun |
| `-c` | `128` | Worker paralel |
| `-r` | `2` | Retry per akun |
| `-proxies` | `proxies.txt` | File proxy |
| `-out` | `accounts.json` | Output JSON |
| `-keys` | `apikey.txt` | Output API key |
| `-keyname` | `1111` | Nama API key di console |
| `-nexkey` | env `NEX_PROXY_KEY` | API key [nextproxy.site](https://console.nextproxy.site) — proxy random per request |

### nextproxy.site (opsional)

Kalau punya key nextproxy, proxy diambil otomatis tiap request (500 req/menit). File `proxies.txt` dipakai sebagai fallback kalau API error.

```bash
# via flag
.\typesafe-create.exe -n 10 -c 8 -nexkey nex_live_xxx

# via env (gak ketulis di history shell)
set NEX_PROXY_KEY=nex_live_xxx
.\typesafe-create.exe -n 10 -c 8
```

### .env (opsional)

Taruh key di `.env` (sudah di-gitignore):

```
NEX_PROXY_KEY=nex_live_xxx
```

Auto ke-load tiap run — tinggal:

```bash
.\typesafe-create.exe -n 10 -c 8
```

### 3. Ambil hasil

Dua file muncul setelah selesai:

| File | Isi |
|------|-----|
| `accounts.json` | Data lengkap: email, API key, cookie, organisasi, error (kalau gagal) |
| `apikey.txt` | API key aja — satu per baris, tinggal copy |

Format API key: `apikey_2xxxxxx`

---

## Kenapa Perlu Proxy?

mail.tm membatasi **8 request per detik per IP**.

- **Tanpa proxy** → semua request lewat 1 IP → lambat, sering gagal
- **Pakai proxy** → rate limit dihitung per IP → jauh lebih kencang

Default: 128 worker paralel, 7 QPS per IP proxy.

---

## Konfigurasi

Lewat flag CLI (lihat [Flag](#flag)). Default ada di `main.go`.

Timeout nunggu email masuk: `mailPollMaxWait = 20s`.

---

## Alur Kerja

<p align="center">
  <img src="assets/flow.png" alt="Alur kerja: mail.tm → typesafe.ai → API keys → accounts.json + apikey.txt" width="720" />
</p>

---

## Masalah Umum

**Gagal parse `$ACTION` di halaman login**
Console typesafe.ai ganti struktur. Perlu re-capture manual.

**Banyak gagal / timeout**
Proxy mati atau rate limit. Ganti proxy yang lebih fresh.

**Warning `deployment console berubah`**
Itu cuma warning, bukan error. Script tetap jalan.

---

## Lisensi

Apache-2.0 — lihat [LICENSE](LICENSE).

Attribution: pakai API [mail.tm](https://mail.tm) — tolong link balik ke mereka.

> **Disclaimer:** Untuk tujuan belajar dan riset. Patuhi ToS layanan yang dipakai.
