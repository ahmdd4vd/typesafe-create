package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	consoleBaseURL      = "https://console.typesafe.ai"
	consoleDeploymentID = "cc6f6dca06537cc04123caaaf50ca5a76d506a92"
	userAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36 Edg/147.0.0.0"
	loginPageURL        = consoleBaseURL + "/login"
)

var magicLinkRe = regexp.MustCompile(
	`https://login\.typesafe\.ai/v1/magic_links/redirect\?public_token=([^&\s"'<>]+)&stytch_token_type=magic_links&token=([^&\s"'<>]+)`,
)

type loginAction struct {
	ActionID string
	Index    string
	Blob     string
}

var (
	loginCache *loginAction
	loginMu    sync.Mutex
	dplWarned  bool
)

func newHTTPClient(proxy string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     30 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   6 * time.Second, // proxy mati → fail cepat
			KeepAlive: 15 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   6 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			CurvePreferences:   []tls.CurveID{tls.X25519, tls.CurveP256},
			InsecureSkipVerify: false,
		},
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("terlalu banyak redirect")
			}
			return nil
		},
	}, nil
}

func getLoginAction(client *http.Client, refresh bool) (*loginAction, error) {
	loginMu.Lock()
	defer loginMu.Unlock()

	if !refresh && loginCache != nil {
		cp := *loginCache
		return &cp, nil
	}

	req, err := http.NewRequest(http.MethodGet, loginPageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "text/html,application/xhtml+xml")
	req.Header.Set("user-agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	pageBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	page := string(pageBytes)

	if m := regexp.MustCompile(`dpl=([0-9a-f]{20,})`).FindStringSubmatch(page); m != nil {
		if m[1] != consoleDeploymentID && !dplWarned {
			logf("Perhatian: deployment console berubah %s -> %s", consoleDeploymentID, m[1])
			dplWarned = true
		}
	}

	pos := strings.Index(page, `type="email"`)
	if pos < 0 {
		pos = len(page)
	}
	idxRe := regexp.MustCompile(`\\?\$ACTION_(\d+):0`)
	idxMatches := idxRe.FindAllStringSubmatch(page[:pos], -1)
	if len(idxMatches) == 0 {
		return nil, fmt.Errorf("halaman login: action index tidak ditemukan, coba update script")
	}
	index := idxMatches[len(idxMatches)-1][1]

	refRe := regexp.MustCompile(`\\?\$ACTION_` + index + `:0" value="([^"]+)"`)
	refMatch := refRe.FindStringSubmatch(page)
	if refMatch == nil {
		return nil, fmt.Errorf("halaman login: ref action tidak ditemukan")
	}
	refRaw := html.UnescapeString(refMatch[1])
	var ref struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(refRaw), &ref); err != nil {
		return nil, fmt.Errorf("halaman login: parse ref action: %w", err)
	}

	blobRe := regexp.MustCompile(`\\?\$ACTION_` + index + `:2" value="([^"]+)"`)
	blobMatch := blobRe.FindStringSubmatch(page)
	if blobMatch == nil {
		return nil, fmt.Errorf("halaman login: blob action tidak ditemukan, coba update script")
	}

	loginCache = &loginAction{
		ActionID: ref.ID,
		Index:    index,
		Blob:     html.UnescapeString(blobMatch[1]),
	}
	cp := *loginCache
	return &cp, nil
}

func sendMagicLink(client *http.Client, email string) (map[string]any, error) {
	var lastStatus int
	var lastRedirect string

	for _, refresh := range []bool{false, true} {
		action, err := getLoginAction(client, refresh)
		if err != nil {
			if !refresh {
				continue
			}
			return nil, err
		}

		boundary := "----WebKitFormBoundary" + randomLocalPart(16)
		idx := action.Index
		body := fmt.Sprintf(
			"--%s\r\nContent-Disposition: form-data; name=\"1\"\r\n\r\n%s\r\n--%s\r\nContent-Disposition: form-data; name=\"_%s_email\"\r\n\r\n%s\r\n--%s\r\nContent-Disposition: form-data; name=\"0\"\r\n\r\n[\"$@1\",\"$K%s\"]\r\n--%s--\r\n",
			boundary, action.Blob, boundary, idx, email, boundary, idx, boundary,
		)

		req, err := http.NewRequest(http.MethodPost, loginPageURL, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "multipart/form-data; boundary="+boundary)
		req.Header.Set("next-action", action.ActionID)
		req.Header.Set("accept", "text/x-component")
		req.Header.Set("origin", consoleBaseURL)
		req.Header.Set("referer", loginPageURL)
		req.Header.Set("sec-fetch-site", "same-origin")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("user-agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		lastStatus = resp.StatusCode
		lastRedirect = resp.Header.Get("x-action-redirect")
		if resp.StatusCode == 200 && strings.Contains(lastRedirect, "sent=true") {
			return map[string]any{
				"action_id":         action.ActionID,
				"form_index":        idx,
				"x_action_redirect": lastRedirect,
			}, nil
		}
	}
	return nil, fmt.Errorf("gagal kirim magic link: HTTP %d redirect=%q", lastStatus, lastRedirect)
}

func authCallback(client *http.Client, token string) (map[string]any, error) {
	payload := map[string]any{
		"token":          token,
		"tokenType":      "magic_links",
		"preferredOrgId": nil,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, consoleBaseURL+"/api/auth/callback", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "*/*")
	req.Header.Set("origin", consoleBaseURL)
	req.Header.Set("referer", fmt.Sprintf("%s/auth/callback?stytch_token_type=magic_links&token=%s", consoleBaseURL, token))
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("user-agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("auth callback HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func createAPIKey(client *http.Client, name string) (map[string]any, error) {
	b, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequest(http.MethodPost, consoleBaseURL+"/api/api-keys", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "*/*")
	req.Header.Set("origin", consoleBaseURL)
	req.Header.Set("referer", consoleBaseURL+"/keys")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("user-agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("buat api key HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cookiesMap(client *http.Client) map[string]string {
	out := map[string]string{}
	u, _ := url.Parse(consoleBaseURL)
	if client.Jar == nil || u == nil {
		return out
	}
	for _, c := range client.Jar.Cookies(u) {
		out[c.Name] = c.Value
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
