package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

const (
	defaultAccountCount = 612
	defaultConcurrency  = 128
	mailPollInterval    = 250 * time.Millisecond
	mailPollMaxWait     = 20 * time.Second
	requestTimeout      = 30 * time.Second
	defaultProxiesPath  = "proxies.txt"
	defaultOutputJSON   = "accounts.json"
	defaultOutputAPIKey = "apikey.txt"
)

var (
	accountCount      int
	concurrency       int
	maxRetriesPerAcct int
	proxiesPath       string
	outputJSONPath    string
	outputAPIKeyPath  string
	apiKeyName        string
)

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[%s] %s", time.Now().Format("15:04:05"), msg)
}

type record map[string]any

func registerOne(slot int, proxies *ProxyPool) record {
	proxy := proxies.Acquire()
	defer proxies.Release(proxy)

	sClient, err := newHTTPClient(proxy)
	if err != nil {
		return record{"status": "failed", "slot": slot, "error": err.Error()}
	}
	mClient, err := newHTTPClient(proxy)
	if err != nil {
		return record{"status": "failed", "slot": slot, "error": err.Error()}
	}
	mailKey := proxyKey(proxy)

	started := time.Now()
	rec := record{
		"status":        "running",
		"slot":          slot,
		"proxy":         proxyVia(proxy),
		"registered_at": time.Now().UTC().Format(time.RFC3339),
	}

	defer func() {
		rec["finished_at"] = time.Now().UTC().Format(time.RFC3339)
		rec["duration_seconds"] = round2(time.Since(started).Seconds())
	}()

	mb, err := createMailbox(mClient, mailKey)
	if err != nil {
		rec["status"] = "failed"
		rec["error"] = err.Error()
		rec["cookies"] = cookiesMap(sClient)
		return rec
	}
	rec["mailbox"] = map[string]any{"id": mb.ID, "email": mb.Email, "token": mb.Token}
	rec["email"] = mb.Email

	sentAt := time.Now().UTC()
	sent, err := sendMagicLink(sClient, mb.Email)
	if err != nil {
		rec["status"] = "failed"
		rec["error"] = err.Error()
		rec["cookies"] = cookiesMap(sClient)
		return rec
	}
	rec["send_magic_link"] = sent

	link, err := waitMagicLink(mClient, mailKey, mb, sentAt)
	if err != nil {
		rec["status"] = "failed"
		rec["error"] = err.Error()
		rec["cookies"] = cookiesMap(sClient)
		return rec
	}
	rec["magic_link"] = map[string]any{
		"email_id":     link.EmailID,
		"sender":       link.Sender,
		"subject":      link.Subject,
		"created_at":   link.CreatedAt,
		"public_token": link.PublicToken,
		"token":        link.Token,
		"magic_link":   link.MagicLink,
	}

	cb, err := authCallback(sClient, link.Token)
	if err != nil {
		rec["status"] = "failed"
		rec["error"] = err.Error()
		rec["cookies"] = cookiesMap(sClient)
		return rec
	}
	rec["callback"] = cb
	rec["user_id"] = cb["userId"]
	rec["backend_user_id"] = cb["backendUserId"]

	orgID := cb["selectedOrgId"]
	orgName := any(nil)
	if memberships, ok := cb["org_memberships"].([]any); ok && len(memberships) > 0 {
		if m0, ok := memberships[0].(map[string]any); ok {
			if org, ok := m0["org"].(map[string]any); ok {
				if id, ok := org["id"].(string); ok && id != "" {
					orgID = id
				}
				orgName = org["name"]
			}
		}
	}
	rec["organization_id"] = orgID
	rec["organization_name"] = orgName

	key, err := createAPIKey(sClient, apiKeyName)
	if err != nil {
		rec["status"] = "failed"
		rec["error"] = err.Error()
		rec["cookies"] = cookiesMap(sClient)
		return rec
	}
	rec["api_key"] = key["api_key"]
	rec["api_key_id"] = key["id"]
	rec["api_key_created"] = key["created"]
	rec["cookies"] = cookiesMap(sClient)
	rec["status"] = "ok"
	return rec
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func main() {
	log.SetFlags(0)

	flag.IntVar(&accountCount, "n", defaultAccountCount, "jumlah akun")
	flag.IntVar(&concurrency, "c", defaultConcurrency, "worker paralel")
	flag.IntVar(&maxRetriesPerAcct, "r", 2, "retry per akun")
	flag.StringVar(&proxiesPath, "proxies", defaultProxiesPath, "file proxy")
	flag.StringVar(&outputJSONPath, "out", defaultOutputJSON, "output JSON")
	flag.StringVar(&outputAPIKeyPath, "keys", defaultOutputAPIKey, "output API key")
	flag.StringVar(&apiKeyName, "keyname", "1111", "nama API key di console")
	flag.Parse()

	if accountCount < 1 {
		accountCount = 1
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if maxRetriesPerAcct < 0 {
		maxRetriesPerAcct = 0
	}

	proxies := LoadProxies(proxiesPath)

	src := "direct"
	if proxies.Len() > 0 {
		src = fmt.Sprintf("file (%d)", proxies.Len())
	}

	logf("Mulai registrasi %d akun (konkurensi %d, proxy %s, mail.tm %.0f QPS/IP) -> %s + %s",
		accountCount, concurrency, src, mailtmQPS, outputJSONPath, outputAPIKeyPath)

	progress := newProgress(accountCount)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	totalOK := 0

	for i := 1; i <= accountCount; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(slot int) {
			defer wg.Done()
			defer func() { <-sem }()

			var rec record
			for attempt := 1; attempt <= maxRetriesPerAcct+1; attempt++ {
				rec = registerOne(slot, proxies)
				if rec["status"] == "ok" {
					rec["attempt"] = attempt
					break
				}
				rec["attempt"] = attempt
			}

			if err := saveAccount(rec); err != nil {
				logf("  Gagal simpan (diabaikan): %v", err)
			}

			line := progress.add(rec["status"] == "ok")
			email, _ := rec["email"].(string)
			if email == "" {
				email = "-"
			}
			errMsg, _ := rec["error"].(string)
			dur, _ := rec["duration_seconds"].(float64)
			via, _ := rec["proxy"].(string)

			mu.Lock()
			if rec["status"] == "ok" {
				totalOK++
				idx := slot
				switch v := rec["index"].(type) {
				case int:
					idx = v
				case float64:
					idx = int(v)
				}
				apiKey, _ := rec["api_key"].(string)
				logf("#%-3d sukses %6.2fs  %-32s via %-22s %s", idx, dur, email, via, apiKey)
			} else {
				logf("#%-3d gagal  %6.2fs  %-32s via %-22s %s", slot, dur, email, via, errMsg)
			}
			mu.Unlock()
			logf("      %s", line)
		}(i)
	}
	wg.Wait()

	logf("Selesai: sukses %d/%d, total waktu %.1fs -> %s + %s",
		totalOK, accountCount, time.Since(progress.started).Seconds(), outputJSONPath, outputAPIKeyPath)
	if totalOK != accountCount {
		os.Exit(1)
	}
}
