package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	mailtmBaseURL = "https://api.mail.tm"
	mailtmQPS     = 7.0
)

type rateLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func newRateLimiter(qps float64) *rateLimiter {
	return &rateLimiter{interval: time.Duration(float64(time.Second) / qps)}
}

func (r *rateLimiter) acquire() {
	r.mu.Lock()
	now := time.Now()
	wait := r.next.Sub(now)
	if wait > 0 {
		r.mu.Unlock()
		time.Sleep(wait)
		r.mu.Lock()
		now = time.Now()
	}
	r.next = now.Add(r.interval)
	r.mu.Unlock()
}

type multiRateLimiter struct {
	qps      float64
	mu       sync.Mutex
	limiters map[string]*rateLimiter
}

func newMultiRateLimiter(qps float64) *multiRateLimiter {
	return &multiRateLimiter{qps: qps, limiters: map[string]*rateLimiter{}}
}

func (m *multiRateLimiter) acquire(key string) {
	m.mu.Lock()
	lim, ok := m.limiters[key]
	if !ok {
		lim = newRateLimiter(m.qps)
		m.limiters[key] = lim
	}
	m.mu.Unlock()
	lim.acquire()
}

var mailtmLimiter = newMultiRateLimiter(mailtmQPS)

var (
	mailtmDomain     string
	mailtmDomainOnce sync.Once
	mailtmDomainErr  error
)

type mailbox struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"-"`
	Token    string `json:"token"`
}

func mailtmRequest(client *http.Client, rateKey, method, path string, body any, token string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		mailtmLimiter.acquire(rateKey)

		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			rdr = bytes.NewReader(b)
		}

		req, err := http.NewRequest(method, mailtmBaseURL+path, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("accept", "*/*")
		if body != nil {
			req.Header.Set("content-type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
			continue
		}

		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 {
			time.Sleep(time.Duration(500*(attempt+1)) * time.Millisecond)
			if time.Duration(500*(attempt+1))*time.Millisecond > 5*time.Second {
				time.Sleep(5 * time.Second)
			}
			continue
		}
		if resp.StatusCode >= 500 {
			time.Sleep(time.Duration(300*(attempt+1)) * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 400 {
			snippet := string(data)
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			return nil, fmt.Errorf("mail.tm %s %s: HTTP %d %s", method, path, resp.StatusCode, snippet)
		}
		if len(data) == 0 {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("mail.tm %s %s gagal: %v", method, path, lastErr)
}

func getMailtmDomain(client *http.Client, rateKey string) (string, error) {
	if mailtmDomain != "" {
		return mailtmDomain, nil
	}

	data, err := mailtmRequest(client, rateKey, http.MethodGet, "/domains", nil, "")
	if err != nil {
		return "", err
	}
	members, _ := data["hydra:member"].([]any)
	var best string
	for _, raw := range members {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		active, _ := m["isActive"].(bool)
		private, _ := m["isPrivate"].(bool)
		domain, _ := m["domain"].(string)
		if domain == "" || !active || private {
			continue
		}
		best = domain
		break
	}
	if best == "" {
		for _, raw := range members {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			domain, _ := m["domain"].(string)
			if domain != "" {
				best = domain
				break
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("mail.tm: tidak ada domain aktif")
	}
	mailtmDomain = best
	return best, nil
}

func randomLocalPart(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func createMailbox(client *http.Client, rateKey string) (*mailbox, error) {
	domain, err := getMailtmDomain(client, rateKey)
	if err != nil {
		return nil, err
	}

	password := randomLocalPart(16)
	var address string
	var account map[string]any
	var lastErr error

	for i := 0; i < 4; i++ {
		address = randomLocalPart(12) + "@" + domain
		account, err = mailtmRequest(client, rateKey, http.MethodPost, "/accounts", map[string]string{
			"address":  address,
			"password": password,
		}, "")
		if err == nil && account != nil {
			break
		}
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "409") {
				continue
			}
			return nil, err
		}
		lastErr = fmt.Errorf("respons kosong/null dari mail.tm")
	}
	if err != nil {
		return nil, fmt.Errorf("mail.tm create account gagal: %w", err)
	}
	if account == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("mail.tm create account gagal: %w", lastErr)
		}
		return nil, fmt.Errorf("mail.tm create account gagal: akun kosong")
	}

	tok, err := mailtmRequest(client, rateKey, http.MethodPost, "/token", map[string]string{
		"address":  address,
		"password": password,
	}, "")
	if err != nil {
		return nil, err
	}
	token, _ := tok["token"].(string)
	if token == "" {
		return nil, fmt.Errorf("mail.tm: token kosong")
	}
	id, _ := account["id"].(string)

	return &mailbox{ID: id, Email: address, Password: password, Token: token}, nil
}

func parseISO(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, ts); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type magicLink struct {
	EmailID     string `json:"email_id"`
	Sender      string `json:"sender"`
	Subject     string `json:"subject"`
	CreatedAt   string `json:"created_at"`
	PublicToken string `json:"public_token"`
	Token       string `json:"token"`
	MagicLink   string `json:"magic_link"`
}

func waitMagicLink(client *http.Client, rateKey string, mb *mailbox, after time.Time) (*magicLink, error) {
	deadline := time.Now().Add(mailPollMaxWait)
	for {
		data, err := mailtmRequest(client, rateKey, http.MethodGet, "/messages", nil, mb.Token)
		if err == nil {
			members, _ := data["hydra:member"].([]any)
			type item struct {
				id      string
				created time.Time
				from    string
				subject string
				has     bool
			}
			var items []item
			for _, raw := range members {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				it := item{}
				it.id, _ = m["id"].(string)
				it.subject, _ = m["subject"].(string)
				if ct, _ := m["createdAt"].(string); ct != "" {
					it.created, it.has = parseISO(ct)
				}
				if frm, ok := m["from"].(map[string]any); ok {
					it.from, _ = frm["address"].(string)
				}
				items = append(items, it)
			}
			// sort desc by created
			for i := 0; i < len(items); i++ {
				for j := i + 1; j < len(items); j++ {
					if items[j].created.After(items[i].created) {
						items[i], items[j] = items[j], items[i]
					}
				}
			}

			for _, it := range items {
				if it.has && !it.created.After(after) {
					continue
				}
				tag := strings.ToLower(it.from + it.subject)
				if !strings.Contains(tag, "typesafe") {
					continue
				}
				mail, err := mailtmRequest(client, rateKey, http.MethodGet, "/messages/"+it.id, nil, mb.Token)
				if err != nil {
					continue
				}
				text, _ := mail["text"].(string)
				hay := text
				switch htmlVal := mail["html"].(type) {
				case string:
					hay += "\n" + htmlVal
				case []any:
					for _, part := range htmlVal {
						if s, ok := part.(string); ok {
							hay += "\n" + s
						}
					}
				}
				if m := magicLinkRe.FindStringSubmatch(hay); m != nil {
					sender := it.from
					if frm, ok := mail["from"].(map[string]any); ok {
						if a, ok := frm["address"].(string); ok && a != "" {
							sender = a
						}
					}
					created, _ := mail["createdAt"].(string)
					subject, _ := mail["subject"].(string)
					mailID, _ := mail["id"].(string)
					return &magicLink{
						EmailID:     mailID,
						Sender:      sender,
						Subject:     subject,
						CreatedAt:   created,
						PublicToken: m[1],
						Token:       m[2],
						MagicLink:   m[0],
					}, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout menunggu magic link (%s)", mailPollMaxWait)
		}
		time.Sleep(mailPollInterval)
	}
}
