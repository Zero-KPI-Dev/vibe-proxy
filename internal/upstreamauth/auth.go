package upstreamauth

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type SecretRef string

func (s SecretRef) Resolve() (string, error) {
	raw := string(s)
	switch {
	case strings.HasPrefix(raw, "env:"):
		return os.Getenv(strings.TrimPrefix(raw, "env:")), nil
	case strings.HasPrefix(raw, "literal:"):
		return strings.TrimPrefix(raw, "literal:"), nil
	case raw == "":
		return "", nil
	default:
		return raw, nil
	}
}

type Profile struct {
	Type    string               `yaml:"type" json:"type"`
	Token   SecretRef            `yaml:"token" json:"token,omitempty"`
	Header  string               `yaml:"header" json:"header,omitempty"`
	Value   SecretRef            `yaml:"value" json:"value,omitempty"`
	Headers map[string]SecretRef `yaml:"headers" json:"headers,omitempty"`
	Query   map[string]SecretRef `yaml:"query" json:"query,omitempty"`
}

func (p Profile) Apply(r *http.Request) *ir.GatewayError {
	switch p.Type {
	case "", "none":
		return nil
	case "bearer":
		token, err := p.Token.Resolve()
		if err != nil || token == "" {
			return authErr("missing_bearer_token")
		}
		r.Header.Set("Authorization", "Bearer "+token)
	case "api_key_header":
		value, err := p.Value.Resolve()
		if err != nil || value == "" || p.Header == "" {
			return authErr("missing_api_key_header")
		}
		r.Header.Set(p.Header, value)
	case "custom_headers":
		for k, ref := range p.Headers {
			v, err := ref.Resolve()
			if err != nil {
				return authErr("secret_resolution_failed")
			}
			if v != "" {
				r.Header.Set(k, v)
			}
		}
	case "custom_query":
		q := r.URL.Query()
		for k, ref := range p.Query {
			v, err := ref.Resolve()
			if err != nil {
				return authErr("secret_resolution_failed")
			}
			if v != "" {
				q.Set(k, v)
			}
		}
		r.URL.RawQuery = q.Encode()
	default:
		return &ir.GatewayError{StatusCode: 500, Kind: "config_error", Code: "unsupported_upstream_auth", Message: "Unsupported upstream auth profile."}
	}
	return nil
}

func ApplyQuery(base string, query map[string]SecretRef) (string, *ir.GatewayError) {
	u, err := url.Parse(base)
	if err != nil {
		return "", &ir.GatewayError{StatusCode: 500, Kind: "config_error", Code: "invalid_upstream_url", Message: "Invalid upstream URL."}
	}
	q := u.Query()
	for k, ref := range query {
		v, err := ref.Resolve()
		if err != nil {
			return "", authErr("secret_resolution_failed")
		}
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func authErr(code string) *ir.GatewayError {
	return &ir.GatewayError{StatusCode: 503, Kind: "upstream_auth_error", Code: code, Message: "Upstream authentication is not configured correctly."}
}
