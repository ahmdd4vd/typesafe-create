package main

import (
	"bufio"
	"os"
	"strings"
)

type ProxyPool struct {
	proxies []string
	tokens  chan string
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
		line = strings.TrimPrefix(line, "\ufeff")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "http://" + line
		}
		p.proxies = append(p.proxies, line)
	}
	if len(p.proxies) > 0 {
		p.tokens = make(chan string, len(p.proxies))
		for _, px := range p.proxies {
			p.tokens <- px
		}
	}
	return p
}

func (p *ProxyPool) Len() int {
	return len(p.proxies)
}

// Acquire blocks sampai ada proxy bebas (1 koneksi per proxy).
// Return "" kalau mode direct (tanpa proxy).
func (p *ProxyPool) Acquire() string {
	if p.tokens == nil {
		return ""
	}
	return <-p.tokens
}

func (p *ProxyPool) Release(proxy string) {
	if proxy == "" || p.tokens == nil {
		return
	}
	select {
	case p.tokens <- proxy:
	default:
	}
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
