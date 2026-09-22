package main

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

type ProxyPool struct {
	mu      sync.Mutex
	proxies []string
	index   int
}

func LoadProxies(path string) *ProxyPool {
	p := &ProxyPool{}
	f, err := os.Open(path)
	if err != nil {
		return p
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimPrefix(line, "\ufeff") // UTF-8 BOM
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "http://" + line
		}
		p.proxies = append(p.proxies, line)
	}
	return p
}

func (p *ProxyPool) Len() int {
	return len(p.proxies)
}

func (p *ProxyPool) Acquire() string {
	if len(p.proxies) == 0 {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	proxy := p.proxies[p.index%len(p.proxies)]
	p.index++
	return proxy
}

func proxyKey(proxy string) string {
	if proxy == "" {
		return "direct"
	}
	if i := strings.LastIndex(proxy, "@"); i >= 0 && i+1 < len(proxy) {
		return proxy[i+1:]
	}
	return proxy
}

func proxyVia(proxy string) string {
	k := proxyKey(proxy)
	if strings.Contains(k, " ") || strings.HasPrefix(k, "#") {
		return "direct"
	}
	return k
}
