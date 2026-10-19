package browser

import (
	"context"
	"os"
	"sync"

	"github.com/chromedp/chromedp"
	"github.com/gi-runner/gi/internal/engine"
)

type page struct {
	ctx    context.Context //nolint:containedctx // chromedp context outlives one action
	cancel context.CancelFunc
	mu     sync.Mutex
	ready  bool
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
	tab, cancelTab := chromedp.NewContext(alloc)
	return &page{ctx: tab, cancel: func() {
		cancelTab()
		cancelAlloc()
	}}, nil
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

// act keeps the browser on p.ctx. A later action uses a child, so cancelling
// the caller stops that action and leaves the browser open.
func (p *page) act(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	first := !p.ready
	p.ready = true
	p.mu.Unlock()
	if first {
		return p.first(ctx, fn)
	}
	c, cancel := context.WithCancel(p.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return fn(c) //nolint:contextcheck // c carries the browser and stops when the caller does
}

func (p *page) first(ctx context.Context, fn func(context.Context) error) error {
	done := make(chan error, 1)
	go func() { done <- fn(p.ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done:
			return err
		default:
			if p.cancel != nil {
				p.cancel()
			}
			<-done
			return ctx.Err()
		}
	}
}

func (p *page) Close() error {
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}
