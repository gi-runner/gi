package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/value"
)

type auth struct {
	kind     string
	grant    string
	user     string
	pass     string
	token    string
	refresh  string
	tokenURL string
	clientID string
	secret   string
	scope    string
	until    time.Time
}

func (a auth) cacheKey() string {
	return strings.Join([]string{a.grant, a.tokenURL, a.clientID, a.secret, a.scope, a.user, a.pass}, "\x00")
}

func (a auth) header() (string, bool) {
	switch a.kind {
	case "basic":
		tok := base64.StdEncoding.EncodeToString([]byte(a.user + ":" + a.pass))
		return "Basic " + tok, true
	case "bearer", "oauth2":
		if a.token == "" {
			return "", false
		}
		return "Bearer " + a.token, true
	default:
		return "", false
	}
}

func (e *exec) setAuth(v any) error {
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("auth expects an object")
	}
	switch value.Sprint(m["type"]) {
	case "basic":
		e.sess.auth = auth{kind: "basic", user: value.Sprint(m["username"]), pass: value.Sprint(m["password"])}
	case "bearer":
		e.sess.auth = auth{kind: "bearer", token: value.Sprint(m["token"])}
	case "oauth2":
		next, err := oauthFrom(m)
		if err != nil {
			return err
		}
		e.sess.auth = next
	case "":
		return errors.New("auth requires type")
	default:
		return fmt.Errorf("unknown auth type %q", value.Sprint(m["type"]))
	}
	return nil
}

func oauthFrom(m map[string]any) (auth, error) {
	tokenURL := value.Sprint(m["accessTokenUrl"])
	id := value.Sprint(m["clientId"])
	secret := value.Sprint(m["clientSecret"])
	if tokenURL == "" || id == "" || secret == "" {
		return auth{}, errors.New("oauth2 requires accessTokenUrl, clientId, and clientSecret")
	}
	grant := value.Sprint(m["grantType"])
	if grant == "" {
		grant = "client_credentials"
	}
	switch grant {
	case "client_credentials", "password", "refresh_token":
	default:
		return auth{}, fmt.Errorf("unknown oauth2 grantType %q", grant)
	}
	user := value.Sprint(m["username"])
	pass := value.Sprint(m["password"])
	refresh := value.Sprint(m["refreshToken"])
	switch grant {
	case "password":
		if user == "" || pass == "" {
			return auth{}, errors.New("oauth2 password grant requires username and password")
		}
	case "refresh_token":
		if refresh == "" {
			return auth{}, errors.New("oauth2 refresh_token grant requires refreshToken")
		}
	case "client_credentials":
		if user != "" || pass != "" || refresh != "" {
			return auth{}, errors.New("oauth2 client credentials takes accessTokenUrl, clientId, clientSecret, and scope")
		}
	}
	return auth{
		kind: "oauth2", grant: grant, user: user, pass: pass, refresh: refresh,
		tokenURL: tokenURL, clientID: id, secret: secret, scope: value.Sprint(m["scope"]),
	}, nil
}

func (e *exec) ensureToken(ctx context.Context, requestURL string) error {
	if e.sess.auth.kind != "oauth2" {
		return nil
	}
	if e.sess.auth.token != "" && (e.sess.auth.until.IsZero() || time.Now().Before(e.sess.auth.until)) {
		return nil
	}
	if e.opt.Tokens == nil {
		access, refresh, until, err := e.fetchToken(ctx, requestURL, e.sess.auth.grant == "refresh_token")
		if err != nil {
			return err
		}
		e.sess.auth.token = access
		e.sess.auth.until = until
		if refresh != "" {
			e.sess.auth.refresh = refresh
		}
		return nil
	}
	entry := e.opt.Tokens.entry(e.sess.auth.cacheKey())
	entry.mu.Lock()
	defer entry.mu.Unlock()
	now := time.Now()
	if entry.access != "" && (entry.until.IsZero() || now.Before(entry.until)) {
		e.sess.auth.token = entry.access
		e.sess.auth.until = entry.until
		if entry.refresh != "" {
			e.sess.auth.refresh = entry.refresh
		}
		return nil
	}
	if entry.refresh != "" {
		e.sess.auth.refresh = entry.refresh
	}
	useRefresh := entry.refresh != "" || (entry.access == "" && e.sess.auth.grant == "refresh_token")
	access, refresh, until, err := e.fetchToken(ctx, requestURL, useRefresh)
	if err != nil {
		return err
	}
	entry.access = access
	entry.until = until
	if refresh != "" {
		entry.refresh = refresh
	} else if entry.refresh == "" {
		entry.refresh = e.sess.auth.refresh
	}
	e.sess.auth.token = access
	e.sess.auth.until = until
	e.sess.auth.refresh = entry.refresh
	return nil
}

func (e *exec) fetchToken(ctx context.Context, requestURL string, refresh bool) (string, string, time.Time, error) {
	raw, err := resolveTokenURL(e.sess.auth.tokenURL, requestURL)
	if err != nil {
		return "", "", time.Time{}, err
	}
	form := url.Values{}
	form.Set("client_id", e.sess.auth.clientID)
	form.Set("client_secret", e.sess.auth.secret)
	if e.sess.auth.scope != "" {
		form.Set("scope", e.sess.auth.scope)
	}
	if refresh {
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", e.sess.auth.refresh)
	} else {
		form.Set("grant_type", e.sess.auth.grant)
		if e.sess.auth.grant == "password" {
			form.Set("username", e.sess.auth.user)
			form.Set("password", e.sess.auth.pass)
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, e.sess.timeout)
	defer cancel()
	reply, err := e.opt.HTTP.Send(callCtx, Call{
		Method: "POST",
		URL:    raw,
		Header: map[string][]string{
			"Accept":       {"application/json"},
			"Content-Type": {"application/x-www-form-urlencoded"},
		},
		Body:      []byte(form.Encode()),
		Follow:    true,
		Transport: e.sess.transport,
	})
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	if reply.Status < 200 || reply.Status >= 300 {
		return "", "", time.Time{}, fmt.Errorf("token request: status %d", reply.Status)
	}
	v, err := value.Parse(reply.Body)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	body, ok := value.AsMap(v)
	if !ok {
		return "", "", time.Time{}, errors.New("token request: response is not an object")
	}
	tok := value.Sprint(body["access_token"])
	if tok == "" {
		return "", "", time.Time{}, errors.New("token request: access_token is missing")
	}
	var until time.Time
	if n, ok := value.AsInt(body["expires_in"]); ok && n > 0 {
		d := time.Duration(n) * time.Second
		if d > time.Second {
			d -= time.Second
		}
		until = time.Now().Add(d)
	}
	return tok, value.Sprint(body["refresh_token"]), until, nil
}

func resolveTokenURL(raw, requestURL string) (string, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw, nil
	}
	u, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("token url %q is relative and the request url has no host", raw)
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return u.Scheme + "://" + u.Host + raw, nil
}
