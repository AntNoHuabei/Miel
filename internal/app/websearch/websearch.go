// Package websearch provides the application-owned, in-process web search
// implementation. Its engine behaviour was ported from the Herdsman search
// integration without importing its AG-UI or Vane runtime dependencies.
package websearch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	defaultTimeout = 12 * time.Second
	cacheTTL       = 5 * time.Minute
	maxSearchBody  = 2 << 20
	maxFetchBody   = 2 << 20
	maxContent     = 128 << 10
)

// Result is a normalized public search result.
type Result struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Engines     []string `json:"engines"`
}

type EngineError struct {
	Engine  string `json:"engine"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type SearchResponse struct {
	Query        string        `json:"query"`
	Results      []Result      `json:"results"`
	EngineErrors []EngineError `json:"engineErrors,omitempty"`
	Cached       bool          `json:"cached"`
}

type Document struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type Config struct {
	ProbeProxy func(context.Context) bool
	Log        func(context.Context, string, ...any)
}

type Service struct {
	mu       sync.Mutex
	cache    map[string]cacheEntry
	backends []*backend
	probe    func(context.Context) bool
	log      func(context.Context, string, ...any)
}

type cacheEntry struct {
	expires time.Time
	value   SearchResponse
}

type backend struct {
	name     string
	find     func(context.Context, *requester, string) ([]Result, error)
	mu       sync.Mutex
	failures int
	openedAt time.Time
}

func New(config Config) *Service {
	probe := config.ProbeProxy
	if probe == nil {
		probe = probeLocalProxy
	}
	log := config.Log
	if log == nil {
		log = func(context.Context, string, ...any) {}
	}
	return &Service{cache: make(map[string]cacheEntry), probe: probe, log: log, backends: defaultBackends()}
}

func (s *Service) Search(ctx context.Context, query string, limit int) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResponse{}, errors.New("搜索关键词不能为空")
	}
	if len([]rune(query)) > 512 {
		return SearchResponse{}, errors.New("搜索关键词不能超过 512 个字符")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	key := fmt.Sprintf("%x:%d", sha256.Sum256([]byte(query)), limit)
	s.mu.Lock()
	if entry, ok := s.cache[key]; ok && time.Now().Before(entry.expires) {
		entry.value.Cached = true
		s.mu.Unlock()
		s.log(ctx, "search.cache_hit", "query_hash", key[:16], "limit", limit)
		return entry.value, nil
	}
	s.mu.Unlock()

	req := newRequester(ctx, s.probe(ctx), s.log)
	defer req.close()
	response := SearchResponse{Query: query, Results: []Result{}, EngineErrors: []EngineError{}}
	seen := make(map[string]int)
	for _, item := range s.backends {
		if !item.allow() {
			s.log(ctx, "search.backend_skipped", "engine", item.name, "reason", "circuit_open")
			continue
		}
		started := time.Now()
		engineCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		results, err := item.find(engineCtx, req, query)
		cancel()
		if err != nil {
			item.failure()
			response.EngineErrors = append(response.EngineErrors, EngineError{Engine: item.name, Kind: "request_failed", Message: compactError(err)})
			s.log(ctx, "search.backend_failed", "engine", item.name, "duration_ms", time.Since(started).Milliseconds(), "error", compactError(err))
			continue
		}
		if len(results) == 0 {
			item.failure()
			response.EngineErrors = append(response.EngineErrors, EngineError{Engine: item.name, Kind: "empty_result", Message: "未返回可用结果"})
			continue
		}
		item.success()
		for _, result := range results {
			result = normalizeResult(result, item.name)
			if result.URL == "" || result.Title == "" {
				continue
			}
			if index, ok := seen[result.URL]; ok {
				response.Results[index].Engines = mergeEngines(response.Results[index].Engines, result.Engines)
				continue
			}
			seen[result.URL] = len(response.Results)
			response.Results = append(response.Results, result)
			if len(response.Results) >= limit {
				break
			}
		}
		s.log(ctx, "search.backend_ready", "engine", item.name, "duration_ms", time.Since(started).Milliseconds(), "results", len(results))
		if len(response.Results) >= limit {
			break
		}
	}
	if len(response.Results) == 0 {
		return response, errors.New("所有搜索引擎均未返回可用结果")
	}
	s.mu.Lock()
	s.cache[key] = cacheEntry{expires: time.Now().Add(cacheTTL), value: response}
	s.mu.Unlock()
	return response, nil
}

func (s *Service) Fetch(ctx context.Context, rawURL string) (Document, error) {
	req := newRequester(ctx, s.probe(ctx), s.log)
	defer req.close()
	body, err := req.get(ctx, rawURL, maxFetchBody)
	if err != nil {
		return Document{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Document{}, fmt.Errorf("解析网页内容失败: %w", err)
	}
	doc.Find("script,style,noscript,svg,canvas,iframe,template").Remove()
	content := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	if content == "" {
		content = strings.Join(strings.Fields(doc.Text()), " ")
	}
	if len(content) > maxContent {
		content = content[:maxContent]
	}
	if content == "" {
		return Document{}, errors.New("网页未包含可提取正文")
	}
	return Document{URL: rawURL, Title: strings.TrimSpace(doc.Find("title").First().Text()), Content: content}, nil
}

func (b *backend) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openedAt.IsZero() || time.Since(b.openedAt) >= 2*time.Minute
}
func (b *backend) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= 2 {
		b.openedAt = time.Now()
	}
}
func (b *backend) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedAt = time.Time{}
}

type requester struct {
	ctx             context.Context
	proxy           bool
	proxied, direct *http.Client
	log             func(context.Context, string, ...any)
}

func newRequester(ctx context.Context, proxy bool, log func(context.Context, string, ...any)) *requester {
	directTransport := http.DefaultTransport.(*http.Transport).Clone()
	directTransport.Proxy = nil
	directTransport.DialContext = publicDialContext
	proxiedTransport := directTransport.Clone()
	proxiedTransport.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: "127.0.0.1:7890"})
	redirect := func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &requester{ctx: ctx, proxy: proxy, direct: &http.Client{Timeout: defaultTimeout, Transport: directTransport, CheckRedirect: redirect}, proxied: &http.Client{Timeout: defaultTimeout, Transport: proxiedTransport, CheckRedirect: redirect}, log: log}
}
func (r *requester) close() { r.direct.CloseIdleConnections(); r.proxied.CloseIdleConnections() }
func (r *requester) get(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	call := func(client *http.Client) ([]byte, error) {
		h, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		h.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125.0 Safari/537.36")
		h.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		response, err := client.Do(h)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			return nil, fmt.Errorf("不允许跟随重定向: HTTP %d", response.StatusCode)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, max+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > max {
			return nil, errors.New("响应超过大小限制")
		}
		return body, nil
	}
	if r.proxy {
		if body, err := call(r.proxied); err == nil {
			r.log(r.ctx, "search.network", "route", "proxy")
			return body, nil
		} else if transportFailure(err) {
			r.log(r.ctx, "search.proxy_failed", "error", compactError(err))
		} else {
			return nil, err
		}
	}
	body, err := call(r.direct)
	if err == nil {
		r.log(r.ctx, "search.network", "route", "direct")
	}
	return body, err
}

func transportFailure(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Timeout() || urlErr.Err != nil
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func publicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if privateIP(ip) {
			return nil, errors.New("拒绝连接本机或内网地址")
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range addresses {
		if privateIP(ip) {
			return nil, errors.New("目标域名解析到本机或内网地址")
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("目标域名未解析到公网地址")
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

func privateIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func probeLocalProxy(ctx context.Context) bool {
	dialer := net.Dialer{Timeout: 250 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:7890")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func defaultBackends() []*backend {
	return []*backend{
		newHTMLBackend("bing", "https://www.bing.com/search?q=%s&mkt=zh-CN&setlang=zh-CN", "li.b_algo", "h2 a", ".b_caption p", "https://www.bing.com"),
		newHTMLBackend("so360", "https://www.so.com/s?q=%s", "li.res-list, .result", "h3 a, .res-title a, .js-title a", ".res-desc, .content, .summary", "https://www.so.com"),
		newHTMLBackend("sogou", "https://www.sogou.com/web?query=%s", ".vrwrap, .reactResult, .rb, .result", "h2 a, h3 a, .pt a, .vr-title a, .title a", ".str_info, .txt-info, .ft, .text-layout, .summary", "https://www.sogou.com"),
		newHTMLBackend("shenma", "https://yz.m.sm.cn/s?q=%s", "a[href]", "", "", "https://yz.m.sm.cn"),
		newHTMLBackend("toutiao", "https://so.toutiao.com/search?keyword=%s", "a[href]", "", "", "https://so.toutiao.com"),
		newHTMLBackend("cctv", "https://search.cctv.com/search.php?qtext=%s&type=web", "h3.tit", "a[href]", "p.bre", "https://search.cctv.com"),
		newHTMLBackend("chinanews", "https://sou.chinanews.com.cn/search.do?q=%s", "a[href]", "", "", "https://sou.chinanews.com.cn"),
		{name: "zhwikipedia", find: wikipediaSearch},
	}
}

func newHTMLBackend(name, endpoint, itemSelector, titleSelector, descSelector, base string) *backend {
	return &backend{name: name, find: func(ctx context.Context, req *requester, query string) ([]Result, error) {
		body, err := req.get(ctx, fmt.Sprintf(endpoint, url.QueryEscape(query)), maxSearchBody)
		if err != nil {
			return nil, err
		}
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		var results []Result
		doc.Find(itemSelector).Each(func(_ int, item *goquery.Selection) {
			title := item
			if titleSelector != "" {
				title = item.Find(titleSelector).First()
			}
			if titleSelector == "" {
				title = item.Find("h1 a, h2 a, h3 a, a[href]").First()
			}
			href, ok := title.Attr("href")
			text := cleanText(title.Text())
			if !ok || text == "" {
				return
			}
			description := ""
			if descSelector != "" {
				description = cleanText(item.Find(descSelector).First().Text())
			}
			results = append(results, Result{Title: text, URL: absoluteURL(base, href), Description: description, Engines: []string{name}})
		})
		return results, nil
	}}
}

func wikipediaSearch(ctx context.Context, req *requester, query string) ([]Result, error) {
	body, err := req.get(ctx, "https://zh.wikipedia.org/w/api.php?action=query&list=search&format=json&utf8=1&srlimit=10&srsearch="+url.QueryEscape(query), maxSearchBody)
	if err != nil {
		return nil, err
	}
	var response struct {
		Query struct {
			Search []struct {
				Title, Snippet string
				PageID         int64 `json:"pageid"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(response.Query.Search))
	for _, item := range response.Query.Search {
		results = append(results, Result{Title: item.Title, URL: fmt.Sprintf("https://zh.wikipedia.org/?curid=%d", item.PageID), Description: htmlText(item.Snippet), Engines: []string{"zhwikipedia"}})
	}
	return results, nil
}

func normalizeResult(result Result, fallback string) Result {
	result.Title = cleanText(result.Title)
	result.Description = cleanText(result.Description)
	result.URL = canonicalURL(result.URL)
	result.Engines = mergeEngines(result.Engines, []string{fallback})
	return result
}
func mergeEngines(values ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range values {
		for _, value := range list {
			if value != "" && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	sort.Strings(out)
	return out
}
func canonicalURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}
func absoluteURL(base, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	root, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return root.ResolveReference(parsed).String()
}
func cleanText(text string) string { return strings.Join(strings.Fields(text), " ") }
func htmlText(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + raw + "</div>"))
	if err != nil {
		return cleanText(raw)
	}
	return cleanText(doc.Text())
}
func compactError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
	if len(value) > 256 {
		return value[:256]
	}
	return value
}
