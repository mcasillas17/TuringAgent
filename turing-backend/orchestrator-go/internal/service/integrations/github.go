package integrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

const maxIntegrationResultBytes = 16 * 1024

func (s *Server) githubClient() *http.Client {
	if s.httpClient != nil {
		return backendegress.NoRedirectClient(s.httpClient)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	lookupIP := s.lookupIP
	if lookupIP == nil {
		lookupIP = net.DefaultResolver.LookupIPAddr
	}
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: true, MaxIdleConns: 16, MaxIdleConnsPerHost: 4,
		IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			resolved, err := backendegress.ResolvePublicAddress(ctx, address, lookupIP)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, resolved)
		},
	}
	return backendegress.NoRedirectClient(&http.Client{Transport: transport, Timeout: 30 * time.Second})
}

func (s *Server) callGitHub(ctx context.Context, toolName string, args map[string]any, credential []byte) (string, error) {
	return s.callGitHubGuarded(ctx, toolName, args, credential, nil)
}

func (s *Server) callGitHubGuarded(
	ctx context.Context,
	toolName string,
	args map[string]any,
	credential []byte,
	immediatelyBeforeDispatch func(context.Context) error,
) (string, error) {
	method, requestURL, body, err := githubRequest(toolName, args)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return "", errors.New("build GitHub request failed")
	}
	request.Header.Set("Authorization", "Bearer "+string(credential))
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := s.githubClient()
	if immediatelyBeforeDispatch != nil {
		if err := immediatelyBeforeDispatch(ctx); err != nil {
			return "", err
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", backendegress.RedactRedirectError(err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("GitHub request failed with HTTP status %d", response.StatusCode)
	}
	// Read far enough past the result boundary to catch a credential that
	// begins just before it. Redaction preserves byte length, so framing still
	// reports truncation from the provider's original response size.
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(maxIntegrationResultBytes+len(credential)+1)))
	if err != nil {
		return "", errors.New("read GitHub response failed")
	}
	redactCredential(raw, credential)
	return frameIntegrationResult(raw)
}

func redactCredential(raw, credential []byte) {
	if len(credential) == 0 {
		return
	}
	// Stored credentials cannot contain control characters. Newlines therefore
	// cannot equal the credential or create it across either replacement edge.
	replacement := bytes.Repeat([]byte{'\n'}, len(credential))
	for offset := 0; offset < len(raw); {
		index := bytes.Index(raw[offset:], credential)
		if index < 0 {
			return
		}
		index += offset
		copy(raw[index:index+len(credential)], replacement)
		offset = index + len(credential)
	}
}

func githubRequest(toolName string, args map[string]any) (string, string, io.Reader, error) {
	connectionID, err := requiredString(args, "connection_id")
	if err != nil || connectionID == "" {
		return "", "", nil, errors.New("connection_id is required")
	}
	owner, err := requiredPathSegment(args, "owner")
	if err != nil {
		return "", "", nil, err
	}
	repo, err := requiredPathSegment(args, "repo")
	if err != nil {
		return "", "", nil, err
	}
	base := repository.GitHubIntegrationEndpoint + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	switch toolName {
	case "github.list_issues":
		values := url.Values{}
		state, present, err := optionalString(args, "state")
		if err != nil {
			return "", "", nil, err
		}
		if present {
			if state != "open" && state != "closed" && state != "all" {
				return "", "", nil, errors.New("state must be open, closed, or all")
			}
			values.Set("state", state)
		}
		limit, present, err := optionalPositiveInt(args, "limit")
		if err != nil {
			return "", "", nil, err
		}
		if present {
			if limit > 100 {
				return "", "", nil, errors.New("limit must not exceed 100")
			}
			values.Set("per_page", strconv.Itoa(limit))
		}
		if query := values.Encode(); query != "" {
			base += "/issues?" + query
		} else {
			base += "/issues"
		}
		return http.MethodGet, base, nil, nil
	case "github.get_issue":
		number, err := requiredPositiveInt(args, "issue_number")
		if err != nil {
			return "", "", nil, err
		}
		return http.MethodGet, base + "/issues/" + strconv.Itoa(number), nil, nil
	case "github.get_file":
		path, err := requiredString(args, "path")
		if err != nil || strings.Trim(path, "/") == "" {
			return "", "", nil, errors.New("path is required")
		}
		segments := strings.Split(strings.Trim(path, "/"), "/")
		for index := range segments {
			if segments[index] == "" || segments[index] == "." || segments[index] == ".." {
				return "", "", nil, errors.New("path is invalid")
			}
			segments[index] = url.PathEscape(segments[index])
		}
		requestURL := base + "/contents/" + strings.Join(segments, "/")
		ref, present, err := optionalString(args, "ref")
		if err != nil {
			return "", "", nil, err
		}
		if present {
			requestURL += "?" + url.Values{"ref": []string{ref}}.Encode()
		}
		return http.MethodGet, requestURL, nil, nil
	case "github.create_comment":
		number, err := requiredPositiveInt(args, "issue_number")
		if err != nil {
			return "", "", nil, err
		}
		comment, err := requiredString(args, "body")
		if err != nil || comment == "" {
			return "", "", nil, errors.New("body is required")
		}
		encoded, err := json.Marshal(map[string]string{"body": comment})
		if err != nil {
			return "", "", nil, errors.New("encode GitHub comment failed")
		}
		return http.MethodPost, base + "/issues/" + strconv.Itoa(number) + "/comments", bytes.NewReader(encoded), nil
	default:
		return "", "", nil, errors.New("unknown integration tool")
	}
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}
func requiredPathSegment(args map[string]any, key string) (string, error) {
	value, err := requiredString(args, key)
	if err != nil {
		return "", err
	}
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "/\\?#") || value == "." || value == ".." {
		return "", fmt.Errorf("%s is invalid", key)
	}
	return value, nil
}
func optionalString(args map[string]any, key string) (string, bool, error) {
	value, ok := args[key]
	if !ok {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", false, fmt.Errorf("%s must be a non-empty string", key)
	}
	return text, true, nil
}
func requiredPositiveInt(args map[string]any, key string) (int, error) {
	value, present, err := optionalPositiveInt(args, key)
	if err != nil || !present {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}
func optionalPositiveInt(args map[string]any, key string) (int, bool, error) {
	value, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	number, ok := value.(float64)
	if !ok || number < 1 || number != float64(int(number)) {
		return 0, false, fmt.Errorf("%s must be a positive integer", key)
	}
	return int(number), true, nil
}

func frameIntegrationResult(raw []byte) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("frame integration result failed")
	}
	marker := "TURING_RETRIEVED_" + hex.EncodeToString(nonce)
	prefix := "BEGIN " + marker + "\n"
	suffix := "\nEND " + marker
	notice := "\n[Result truncated to 16384 bytes on a UTF-8 boundary.]"
	valid := []byte(strings.ToValidUTF8(string(raw), "�"))
	available := maxIntegrationResultBytes - len(prefix) - len(suffix)
	truncated := len(valid) > available
	if truncated {
		available -= len(notice)
		valid = valid[:available]
		for len(valid) > 0 && !utf8.Valid(valid) {
			valid = valid[:len(valid)-1]
		}
	}
	result := prefix + string(valid) + suffix
	if truncated {
		result += notice
	}
	return result, nil
}
