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
	user     string
	pass     string
	token    string
	tokenURL string
	clientID string
	secret   string
	scope    string
	until    time.Time
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
		tokenURL := value.Sprint(m["accessTokenUrl"])
		id := value.Sprint(m["clientId"])
		secret := value.Sprint(m["clientSecret"])
		if tokenURL == "" || id == "" || secret == "" {
			return errors.New("oauth2 requires accessTokenUrl, clientId, and clientSecret")
		}
		scope := ""
		if m["scope"] != nil {
			scope = value.Sprint(m["scope"])
		}
		e.sess.auth = auth{kind: "oauth2", tokenURL: tokenURL, clientID: id, secret: secret, scope: scope}
	case "":
		return errors.New("auth requires type")
	default:
		return fmt.Errorf("unknown auth type %q", value.Sprint(m["type"]))
	}
	return nil
}

func (e *exec) ensureToken(ctx context.Context, requestURL string) error {
	if e.sess.auth.kind != "oauth2" {
		return nil
	}
	if e.sess.auth.token != "" && (e.sess.auth.until.IsZero() || time.Now().Before(e.sess.auth.until)) {
		return nil
	}
	tok, until, err := e.clientToken(ctx, requestURL)
	if err != nil {
		return err
	}
	e.sess.auth.token = tok
	e.sess.auth.until = until
	return nil
}

func (e *exec) clientToken(ctx context.Context, requestURL string) (string, time.Time, error) {
	raw, err := resolveTokenURL(e.sess.auth.tokenURL, requestURL)
	if err != nil {
		return "", time.Time{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", e.sess.auth.clientID)
	form.Set("client_secret", e.sess.auth.secret)
	if e.sess.auth.scope != "" {
		form.Set("scope", e.sess.auth.scope)
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
		return "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	if reply.Status < 200 || reply.Status >= 300 {
		return "", time.Time{}, fmt.Errorf("token request: status %d", reply.Status)
	}
	v, err := value.Parse(reply.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	m, ok := value.AsMap(v)
	if !ok {
		return "", time.Time{}, errors.New("token request: response is not an object")
	}
	tok := value.Sprint(m["access_token"])
	if tok == "" {
		return "", time.Time{}, errors.New("token request: access_token is missing")
	}
	var until time.Time
	if n, ok := value.AsInt(m["expires_in"]); ok && n > 0 {
		d := time.Duration(n) * time.Second
		if d > time.Second {
			d -= time.Second
		}
		until = time.Now().Add(d)
	}
	return tok, until, nil
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
