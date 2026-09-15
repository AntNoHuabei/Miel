package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/AntNoHuabei/Miel/internal/app/websearch"
)

// WebSearchService is the permission-aware application boundary around the
// in-process search implementation.
type WebSearchService struct {
	search *websearch.Service
	lookup func(context.Context, string, string) ([]net.IP, error)
}

func NewWebSearchService() *WebSearchService {
	return &WebSearchService{
		search: websearch.New(websearch.Config{Log: func(ctx context.Context, event string, values ...any) { logInfo(ctx, "websearch."+event, values...) }}),
		lookup: net.DefaultResolver.LookupIP,
	}
}

func (s *WebSearchService) Search(ctx context.Context, query string, limit int) (websearch.SearchResponse, error) {
	if s == nil || s.search == nil {
		return websearch.SearchResponse{}, errors.New("联网搜索服务未初始化")
	}
	if err := authorizeWebSearch(ctx, "search", "search:"+queryHash(query)); err != nil {
		return websearch.SearchResponse{}, err
	}
	started := time.Now()
	response, err := s.search.Search(ctx, query, limit)
	if err != nil {
		logError(ctx, "websearch.search.failed", err, "query_hash", queryHash(query), "query_length", len([]rune(query)), "duration_ms", time.Since(started).Milliseconds())
		return response, err
	}
	logInfo(ctx, "websearch.search.ready", "query_hash", queryHash(query), "query_length", len([]rune(query)), "results", len(response.Results), "cached", response.Cached, "duration_ms", time.Since(started).Milliseconds())
	return response, nil
}

func (s *WebSearchService) Fetch(ctx context.Context, rawURL string) (websearch.Document, error) {
	if s == nil || s.search == nil {
		return websearch.Document{}, errors.New("联网搜索服务未初始化")
	}
	parsed, err := s.safePublicURL(ctx, rawURL)
	if err != nil {
		return websearch.Document{}, err
	}
	if err := authorizeWebSearch(ctx, "fetch", parsed.String()); err != nil {
		return websearch.Document{}, err
	}
	started := time.Now()
	document, err := s.search.Fetch(ctx, parsed.String())
	if err != nil {
		logError(ctx, "websearch.fetch.failed", err, "host", parsed.Hostname(), "duration_ms", time.Since(started).Milliseconds())
		return websearch.Document{}, err
	}
	logInfo(ctx, "websearch.fetch.ready", "host", parsed.Hostname(), "content_length", len(document.Content), "duration_ms", time.Since(started).Milliseconds())
	return document, nil
}

func authorizeWebSearch(ctx context.Context, operation, target string) error {
	service, sessionID, workspace := permissionContext(ctx)
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return errors.New("联网搜索需要可用的交互式权限会话")
	}
	decision, err := service.authorize(ctx, ApprovalRequest{SessionID: sessionID, Tool: "websearch", Operation: operation, WorkspacePath: workspace, Target: target, ScopeRoot: "network", RiskLevel: "medium"})
	if err != nil {
		return fmt.Errorf("联网权限已取消: %w", err)
	}
	if decision == ApprovalDeny {
		return errors.New("用户拒绝了联网搜索权限")
	}
	return nil
}

func (s *WebSearchService) safePublicURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("URL 只能使用 http 或 https")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, errors.New("禁止访问本机或本地域名")
	}
	if ip := net.ParseIP(host); ip != nil {
		if privateNetworkIP(ip) {
			return nil, errors.New("禁止访问本机或内网地址")
		}
		return parsed, nil
	}
	lookup := s.lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIP
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := lookup(lookupCtx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析目标域名失败: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("目标域名未解析到公网地址")
	}
	for _, address := range addresses {
		if privateNetworkIP(address) {
			return nil, errors.New("目标域名解析到本机或内网地址")
		}
	}
	return parsed, nil
}

func privateNetworkIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func queryHash(query string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return fmt.Sprintf("%x", sum[:8])
}
