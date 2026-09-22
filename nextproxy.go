package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	nextProxyBase     = "https://console.nextproxy.site/api/random"
	nextProxyTimeout  = 15 * time.Second
	nextProxyMinGap   = 120 * time.Millisecond // ~500 req/min
	nextProxyMaxRetry = 2
)

type nextProxyClient struct {
	apiKey string
	hc     *http.Client
	mu     sync.Mutex
	last   time.Time
}

type nextProxyPayload struct {
	Status string `json:"status"`
	Proxy  struct {
		IP       string `json:"ip"`
		Port     string `json:"port"`
		Type     string `json:"type"`
		Protocol string `json:"protocol"`
		Status   string `json:"status"`
	} `json:"proxy"`
}

func newNextProxyClient(apiKey string) *nextProxyClient {
	return &nextProxyClient{
		apiKey: apiKey,
		hc:     &http.Client{Timeout: nextProxyTimeout},
	}
}

func (n *nextProxyClient) Fetch() (string, error) {
	n.mu.Lock()
	wait := nextProxyMinGap - time.Since(n.last)
	if wait > 0 {
		n.mu.Unlock()
		time.Sleep(wait)
		n.mu.Lock()
	}
	n.last = time.Now()
	n.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= nextProxyMaxRetry; attempt++ {
		proxy, err := n.fetchOnce()
		if err == nil {
			return proxy, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
	}
	return "", lastErr
}

func (n *nextProxyClient) fetchOnce() (string, error) {
	req, err := http.NewRequest(http.MethodGet, nextProxyBase, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", n.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := n.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nextproxy HTTP %d: %s", resp.StatusCode, truncate(string(body), 120))
	}

	var payload nextProxyPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("nextproxy parse: %w", err)
	}
	if payload.Status != "success" || payload.Proxy.IP == "" || payload.Proxy.Port == "" {
		return "", fmt.Errorf("nextproxy empty: %s", truncate(string(body), 120))
	}

	scheme := payload.Proxy.Protocol
	if scheme == "" {
		scheme = payload.Proxy.Type
	}
	// Go http.Transport: proxy URL hampir selalu http:// (https:// = TLS ke proxy)
	// API nextproxy balikin "https" + port 80 → pakai http proxy biasa
	if scheme == "https" || scheme == "http" || scheme == "" {
		scheme = "http"
	} else {
		scheme = normalizeProxyScheme(scheme)
	}
	return fmt.Sprintf("%s://%s:%s", scheme, payload.Proxy.IP, payload.Proxy.Port), nil
}

func normalizeProxyScheme(s string) string {
	switch s {
	case "http", "https", "socks5", "socks4":
		return s
	case "socks5h":
		return "socks5"
	default:
		return "http"
	}
}
