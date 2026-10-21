package browser

import (
	"context"
	"fmt"
	"iter"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	cdpPage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/gi-runner/gi/internal/engine"
)

// A tab owns one Chrome target: its context, the subscriptions on that
// target, its interception rules, and the cancel that closes it.
type tab struct {
	id      target.ID
	ctx     context.Context //nolint:containedctx // the target outlives one step
	cancel  context.CancelFunc
	ready   bool
	watched bool
	fetch   bool

	mu            sync.Mutex
	frame         string
	rules         []intercept
	dialogArmed   bool
	dialogAccept  bool
	dialogPrompt  string
	dialogMessage string
}

type page struct {
	root   context.Context //nolint:containedctx // parent for targets opened later
	cancel context.CancelFunc
	mu     sync.Mutex
	active *tab
	tabs   []*tab
}

type intercept struct {
	pattern string
	status  int
	body    string
}

func Open(ctx context.Context) (engine.Page, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
	)
	// Restricted CI hosts may need an explicit sandbox opt-out.
	if os.Getenv("GI_CHROME_NO_SANDBOX") == "true" {
		opts = append(opts, chromedp.NoSandbox)
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	tabCtx, cancelTab := chromedp.NewContext(alloc)
	first := &tab{ctx: tabCtx, cancel: cancelTab}
	return &page{
		root:   tabCtx,
		cancel: cancelAlloc,
		active: first,
		tabs:   []*tab{first},
	}, nil
}

func (p *page) Open(ctx context.Context, url string) error {
	return p.act(ctx, func(c context.Context) error {
		return chromedp.Do(c, chromedp.Navigate(url))
	})
}

func (p *page) Click(ctx context.Context, selector string) error {
	return p.act(ctx, func(c context.Context) error {
		return chromedp.Do(c, chromedp.Click(selector))
	})
}

func (p *page) Fill(ctx context.Context, selector, text string) error {
	return p.act(ctx, func(c context.Context) error {
		return chromedp.Do(c, chromedp.SendKeys(selector, text))
	})
}

func (p *page) Text(ctx context.Context, selector string) (string, error) {
	var got string
	err := p.act(ctx, func(c context.Context) error {
		var err error
		got, err = chromedp.Run(c, chromedp.Text(selector))
		return err
	})
	return got, err
}

func (p *page) activeTab() *tab {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// act keeps the target open. The first call initializes it, and every later
// call uses a child so cancelling the step leaves the target open.
func (p *page) act(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := p.activeTab()
	if !t.ready {
		if err := t.init(ctx); err != nil {
			drop(t.cancel)
			return err
		}
		t.watch()
	}
	return t.run(ctx, fn)
}

func (p *page) Close() error {
	p.mu.Lock()
	tabs := p.tabs
	cancel := p.cancel
	p.tabs = nil
	p.mu.Unlock()
	for _, t := range slices.Backward(tabs) {
		t.cancel()
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// run bounds one operation to the step. Cancelling the step does not close the tab.
func (t *tab) run(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return fn(c) //nolint:contextcheck // c carries the target and stops when the step does
}

// Subscribers start only after the target exists. Events also initializes a
// context, so overlapping that with navigation races the first attach.
func (t *tab) watch() {
	if t.watched {
		return
	}
	t.watched = true
	dialogs := chromedp.Events(t.ctx, cdpPage.JavascriptDialogOpening)
	paused := chromedp.Events(t.ctx, fetch.RequestPaused)
	go t.dialogs(dialogs)
	go t.paused(paused)
}

func (t *tab) dialogs(events iter.Seq2[cdpPage.EventJavascriptDialogOpening, error]) {
	for ev, err := range events {
		if err != nil {
			return
		}
		t.answer(ev)
	}
}

func (t *tab) answer(ev cdpPage.EventJavascriptDialogOpening) {
	t.mu.Lock()
	t.dialogMessage = ev.Message
	accept, prompt := true, ""
	if t.dialogArmed {
		accept, prompt = t.dialogAccept, t.dialogPrompt
		t.dialogArmed = false
	}
	t.mu.Unlock()
	_, _ = chromedp.Run(t.ctx, func(ctx context.Context, tgt *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, tgt, cdpPage.HandleJavaScriptDialog, cdpPage.HandleJavaScriptDialogParams{
			Accept: accept, PromptText: prompt,
		})
		return chromedp.Void{}, err
	})
}

func (t *tab) paused(events iter.Seq2[fetch.EventRequestPaused, error]) {
	for ev, err := range events {
		if err != nil {
			return
		}
		t.fulfill(ev)
	}
}

func (t *tab) fulfill(ev fetch.EventRequestPaused) {
	t.mu.Lock()
	var rule intercept
	matched := false
	url := ""
	if ev.Request != nil {
		url = ev.Request.URL
	}
	for _, item := range t.rules {
		if strings.Contains(url, item.pattern) {
			rule, matched = item, true
			break
		}
	}
	t.mu.Unlock()
	_, _ = chromedp.Run(t.ctx, func(ctx context.Context, tgt *chromedp.Target) (chromedp.Void, error) {
		if !matched {
			_, err := cdp.Call(ctx, tgt, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
			return chromedp.Void{}, err
		}
		_, err := cdp.Call(ctx, tgt, fetch.FulfillRequest, fetch.FulfillRequestParams{
			RequestID:    ev.RequestID,
			ResponseCode: int64(rule.status),
			Body:         []byte(rule.body),
			ResponseHeaders: []*fetch.HeaderEntry{{
				Name: "content-type", Value: "text/plain; charset=utf-8",
			}},
		})
		return chromedp.Void{}, err
	})
}

func (t *tab) enableFetch(ctx context.Context) error {
	if t.fetch {
		return nil
	}
	err := t.run(ctx, func(c context.Context) error {
		_, err := chromedp.Run(c, func(ctx context.Context, tgt *chromedp.Target) (chromedp.Void, error) {
			_, err := cdp.Call(ctx, tgt, fetch.Enable, fetch.EnableParams{
				Patterns: []*fetch.RequestPattern{{URLPattern: "*"}},
			})
			return chromedp.Void{}, err
		})
		return err
	})
	if err != nil {
		return err
	}
	t.fetch = true
	return nil
}

func (t *tab) rulesCopy() []intercept {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.rules) == 0 {
		return nil
	}
	out := make([]intercept, len(t.rules))
	copy(out, t.rules)
	return out
}

func (p *page) openWindow(ctx context.Context, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tabCtx, tabCancel := chromedp.NewContext(p.root)
	next := &tab{ctx: tabCtx, cancel: tabCancel, rules: p.activeTab().rulesCopy()}
	if err := next.init(ctx); err != nil {
		drop(tabCancel)
		return err
	}
	if err := p.arm(ctx, next); err != nil {
		drop(tabCancel)
		return err
	}
	err := next.run(ctx, func(c context.Context) error {
		return chromedp.Do(c, chromedp.Navigate(url))
	})
	if err != nil {
		drop(tabCancel)
		return err
	}
	p.adopt(next)
	return nil
}

func (p *page) windows(ctx context.Context) ([]any, error) {
	infos, err := p.targets(ctx)
	if err != nil {
		return nil, err
	}
	var out []any
	index := 0
	for _, info := range infos {
		if info.Type != "page" {
			continue
		}
		out = append(out, map[string]any{
			"index": index, "url": info.URL, "title": info.Title, "id": string(info.TargetID),
		})
		index++
	}
	return out, nil
}

func (p *page) useWindow(ctx context.Context, n int) error {
	infos, err := p.targets(ctx)
	if err != nil {
		return err
	}
	var pages []*target.Info
	for _, info := range infos {
		if info.Type == "page" {
			pages = append(pages, info)
		}
	}
	if n < 0 || n >= len(pages) {
		return errWindow(n)
	}
	id := pages[n].TargetID
	if existing := p.find(id); existing != nil {
		p.mu.Lock()
		p.active = existing
		p.mu.Unlock()
		return nil
	}
	tabCtx, tabCancel := chromedp.NewContext(p.root, chromedp.WithTargetID(id), chromedp.WithDetachOnCancel())
	next := &tab{ctx: tabCtx, cancel: tabCancel, id: id, rules: p.activeTab().rulesCopy()}
	if err := next.init(ctx); err != nil {
		drop(tabCancel)
		return err
	}
	if err := p.arm(ctx, next); err != nil {
		drop(tabCancel)
		return err
	}
	err = next.run(ctx, func(c context.Context) error {
		_, err := chromedp.Run(c, chromedp.Title())
		return err
	})
	if err != nil {
		drop(tabCancel)
		return err
	}
	p.adopt(next)
	return nil
}

// init attaches the target on its own context. That context owns the target
// loop, so a step deadline waits beside the attach and drops the tab if it expires.
func (t *tab) init(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- chromedp.Do(t.ctx, chromedp.Func(func(context.Context, *chromedp.Target) error {
			return nil
		}))
	}()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
		t.ready = true
		if t.id == "" {
			t.id = targetOf(t.ctx) //nolint:contextcheck // the id comes from the initialized target
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// drop closes a tab the step did not keep. Close waits for Chrome, so it must not block the step.
func drop(cancel context.CancelFunc) {
	go cancel()
}

// arm subscribes after the target exists, then enables fetch on that target
// when it already has rules.
func (p *page) arm(ctx context.Context, next *tab) error {
	next.watch()
	if len(next.rules) == 0 {
		return nil
	}
	return next.enableFetch(ctx)
}

func (p *page) adopt(next *tab) {
	p.mu.Lock()
	p.tabs = append(p.tabs, next)
	p.active = next
	p.mu.Unlock()
}

func (p *page) targets(ctx context.Context) ([]*target.Info, error) {
	var infos []*target.Info
	err := p.activeTab().run(ctx, func(c context.Context) error {
		var err error
		infos, err = chromedp.Targets(c)
		return err
	})
	return infos, err
}

func (p *page) find(id target.ID) *tab {
	if id == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}

func targetOf(ctx context.Context) target.ID {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return ""
	}
	return c.Target.TargetID
}

func errWindow(n int) error {
	return fmt.Errorf("window %d is not open", n)
}
