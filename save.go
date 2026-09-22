package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type progress struct {
	mu      sync.Mutex
	total   int
	done    int
	ok      int
	fail    int
	started time.Time
}

func newProgress(total int) *progress {
	return &progress{total: total, started: time.Now()}
}

func (p *progress) add(ok bool) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done++
	if ok {
		p.ok++
	} else {
		p.fail++
	}
	return p.renderLocked()
}

func (p *progress) renderLocked() string {
	elapsed := time.Since(p.started).Seconds()
	avg := 0.0
	if p.done > 0 {
		avg = elapsed / float64(p.done)
	}
	rate := 0.0
	if avg > 0 {
		rate = 60 / avg
	}
	remaining := avg * float64(p.total-p.done)
	pct := 0
	if p.total > 0 {
		pct = p.done * 100 / p.total
	}
	return fmt.Sprintf(
		"progres %d/%d (%d%%) | sukses %d gagal %d | waktu %.1fs | rata-rata %.2fs/akun | %.1f akun/menit | sisa ~%.1fs",
		p.done, p.total, pct, p.ok, p.fail, elapsed, avg, rate, remaining,
	)
}

var saveMu sync.Mutex

func saveAccount(record map[string]any) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	path := outputJSONPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	accounts := []map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		var parsed []map[string]any
		if json.Unmarshal(data, &parsed) == nil {
			accounts = parsed
		}
	}

	// index dari record belum ada — hitung setelah append
	accounts = append(accounts, record)
	for i, item := range accounts {
		item["index"] = i + 1
	}

	payload, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	renamed := false
	for attempt := 0; attempt < 4; attempt++ {
		if err := os.Rename(tmp, path); err == nil {
			renamed = true
			break
		}
		time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
	}
	if !renamed {
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			return err
		}
	}

	// tarik API key saja ke apikey.txt (gampang dicopy user)
	if apiKey, ok := record["api_key"].(string); ok && apiKey != "" {
		if err := appendAPIKey(apiKey); err != nil {
			return err
		}
	}
	return nil
}

func appendAPIKey(key string) error {
	f, err := os.OpenFile(outputAPIKeyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(key + "\n")
	return err
}
