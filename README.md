# typesafe-create

Auto-register akun [typesafe.ai](https://typesafe.ai) + auto-bikin API key. Ditulis dalam Go — ringan, cepat, tanpa dependency eksternal.

Email sementara dari [mail.tm](https://mail.tm) (gratis, no API key).

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

Semua di `main.go`:

| Konstanta | Default | Fungsi |
|-----------|---------|--------|
| `accountCount` | 612 | Jumlah akun yang mau dibuat |
| `concurrency` | 128 | Worker paralel |
| `apiKeyName` | `1111` | Nama API key di console |
| `maxRetriesPerAcct` | 2 | Retry kalau gagal |
| `mailPollMaxWait` | 20s | Timeout nunggu email masuk |

---

## Alur Kerja

```
mail.tm                    typesafe.ai
   │                            │
   │  1. buat email random      │
   │───────────────────────────▶│  2. kirim magic link
   │                            │
   │  3. ambil isi email        │
   │◀───────────────────────────│
   │                            │
   │         4. login pakai token
   │◀──────────────────────────▶│
   │                            │
   │         5. bikin API key
   │◀──────────────────────────▶│
   │                            │
   ▼                            ▼
accounts.json + apikey.txt
```

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

Apache-2.0 — lihat [LICENSE](../LICENSE).

Attribution: pakai API [mail.tm](https://mail.tm) — tolong link balik ke mereka.

> **Disclaimer:** Untuk tujuan belajar dan riset. Patuhi ToS layanan yang dipakai.
