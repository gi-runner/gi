package browser

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gi-runner/gi/internal/value"
)

func interceptBody(args []any) (string, int, error) {
	if len(args) < 2 {
		return "", 0, errors.New("intercept requires a URL substring and a response")
	}
	spec, ok := value.AsMap(args[1])
	if !ok {
		return "", 0, errors.New("intercept response must be an object")
	}
	status := 200
	if raw, ok := spec["status"]; ok {
		n, ok := value.AsInt(raw)
		if !ok || n < 100 || n > 599 {
			return "", 0, errors.New("intercept status must be an HTTP status")
		}
		status = n
	}
	body := spec["body"]
	if s, ok := body.(string); ok || body == nil {
		return s, status, nil
	}
	raw, err := value.Marshal(body)
	if err != nil {
		return "", 0, err
	}
	return string(raw), status, nil
}

func (p *page) runAction[T any](ctx context.Context, action chromedp.Action[T]) (out T, err error) {
	err = p.act(ctx, func(c context.Context) error {
		var actionErr error
		out, actionErr = chromedp.Run(c, action)
		return actionErr
	})
	return out, err
}

// Locators are quoted as data, never spliced into browser JavaScript as code.
func nodeExpression(selector, root string) string {
	if strings.HasPrefix(selector, "/") || strings.HasPrefix(selector, "(") {
		return root + ".evaluate(" + strconv.Quote(selector) + "," + root + ",null,XPathResult.FIRST_ORDERED_NODE_TYPE,null).singleNodeValue"
	}
	if strings.HasPrefix(selector, "{") {
		if end := strings.IndexByte(selector, '}'); end > 0 {
			tag, text := selector[1:end], selector[end+1:]
			partial := strings.HasPrefix(tag, "^")
			tag = strings.TrimPrefix(tag, "^")
			if tag == "" {
				tag = "*"
			}
			comparison := "=== " + strconv.Quote(text)
			if partial {
				comparison = ".includes(" + strconv.Quote(text) + ")"
			}
			return "Array.from(" + root + ".querySelectorAll(" + strconv.Quote(tag) + ")).find(function(e){return e.textContent.trim()" + comparison + "})"
		}
	}
	return root + ".querySelector(" + strconv.Quote(selector) + ")"
}

func (t *tab) rootExpr() string {
	if t.frame == "" {
		return "document"
	}
	return "(" + nodeExpression(t.frame, "document") + ").contentDocument"
}

func (p *page) Action(ctx context.Context, name string, args []any) (any, error) {
	tab := p.activeTab()
	text := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return value.Sprint(args[i])
	}
	selector := text(0)
	node := nodeExpression(selector, tab.rootExpr())
	eval := func(src string) (any, error) { return p.runAction(ctx, chromedp.Evaluate[any](src)) }
	wait := func(src string) (any, error) {
		return p.runAction(ctx, chromedp.Poll[bool](src, chromedp.WithPollingInterval(50*time.Millisecond)))
	}
	element := func(code string) (any, error) {
		return eval("(function(e){if(!e)throw Error('element not found: '+" + strconv.Quote(selector) + ");" + code + "})(" + node + ")")
	}
	switch name {
	case "inputFile":
		if len(args) != 2 {
			return nil, errors.New("inputFile requires a locator and file paths")
		}
		paths, ok := args[1].([]string)
		if !ok {
			return nil, errors.New("inputFile expects resolved file paths")
		}
		if len(paths) == 0 {
			return element("e.value='';e.dispatchEvent(new Event('change',{bubbles:true}));return true")
		}
		return p.runAction(ctx, chromedp.SetUploadFiles(chromedp.JSPath(node), paths))
	case "getUrl":
		return p.runAction(ctx, chromedp.Location())
	case "title":
		return p.runAction(ctx, chromedp.Title())
	case "back":
		return p.runAction(ctx, chromedp.NavigateBack())
	case "forward":
		return p.runAction(ctx, chromedp.NavigateForward())
	case "refresh":
		return p.runAction(ctx, chromedp.Reload())
	case "screenshot":
		return p.runAction(ctx, chromedp.FullScreenshot(100))
	case "exists":
		return eval("Boolean(" + node + ")")
	case "enabled":
		return eval("(function(e){return !!e&&!e.disabled})(" + node + ")")
	case "text":
		return element("return e.innerText===undefined?e.textContent:e.innerText")
	case "html":
		return element("return e.outerHTML")
	case "value":
		if len(args) == 1 {
			return element("return e.value")
		}
		return element("e.value=" + strconv.Quote(text(1)) + ";e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return true")
	case "attribute":
		if len(args) < 2 {
			return nil, errors.New("attribute requires a locator and attribute name")
		}
		return element("return e.getAttribute(" + strconv.Quote(text(1)) + ")")
	case "clear":
		return element("e.value='';e.dispatchEvent(new Event('input',{bubbles:true}));return true")
	case "focus":
		return element("e.focus();return true")
	case "scroll":
		return element("e.scrollIntoView({block:'center'});return true")
	case "select":
		if len(args) < 2 {
			return nil, errors.New("select requires a locator and value")
		}
		return element("e.value=" + strconv.Quote(text(1)) + ";e.dispatchEvent(new Event('change',{bubbles:true}));return true")
	case "click":
		if tab.frame != "" || strings.HasPrefix(selector, "{") {
			return element("e.click();return true")
		}
		return p.runAction(ctx, chromedp.Click(selector))
	case "input":
		if len(args) < 2 {
			return nil, errors.New("input requires a locator and text")
		}
		if tab.frame != "" || strings.HasPrefix(selector, "{") {
			return element("e.focus();e.value=" + strconv.Quote(text(1)) + ";e.dispatchEvent(new Event('input',{bubbles:true}));return true")
		}
		return p.runAction(ctx, chromedp.SendKeys(selector, text(1)))
	case "locate":
		return element("return true")
	case "waitFor":
		return wait("Boolean(" + node + ")")
	case "waitForEnabled":
		return wait("(function(e){return !!e&&!e.disabled})(" + node + ")")
	case "waitForText":
		if len(args) < 2 {
			return nil, errors.New("waitForText requires a locator and text")
		}
		return wait("(function(e){return !!e&&e.textContent.includes(" + strconv.Quote(text(1)) + ")})(" + node + ")")
	case "waitForUrl":
		return wait("location.href.includes(" + strconv.Quote(selector) + ")")
	case "waitUntil":
		return wait(selector)
	case "script":
		if len(args) == 1 {
			return eval(selector)
		}
		return element("return (function(_){return eval(" + strconv.Quote(text(1)) + ")})(e)")
	case "locateAll":
		root := tab.rootExpr()
		list := "Array.from(" + root + ".querySelectorAll(" + strconv.Quote(selector) + "))"
		if strings.HasPrefix(selector, "/") {
			list = "(function(){var r=" + root + ".evaluate(" + strconv.Quote(selector) + "," + root + ",null,XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,null);var a=[];for(var i=0;i<r.snapshotLength;i++)a.push(r.snapshotItem(i));return a})()"
		}
		return eval(list + ".map(function(n){var p='';for(;n&&n.nodeType===1;n=n.parentElement){var i=1;for(var s=n.previousElementSibling;s;s=s.previousElementSibling)if(s.tagName===n.tagName)i++;p='/'+n.tagName.toLowerCase()+'['+i+']'+p}return p})")
	case "frame":
		if selector == "" {
			tab.frame = ""
			return true, nil
		}
		ok, err := eval("Boolean(" + nodeExpression(selector, "document") + "&&" + nodeExpression(selector, "document") + ".contentDocument)")
		if err != nil {
			return nil, err
		}
		if ok != true {
			return nil, fmt.Errorf("frame %s has no document", selector)
		}
		tab.frame = selector
		return true, nil
	case "dialog":
		if len(args) == 0 {
			return nil, errors.New("dialog requires true to accept or false to dismiss")
		}
		accept, ok := args[0].(bool)
		if !ok {
			return nil, errors.New("dialog requires true to accept or false to dismiss")
		}
		tab.mu.Lock()
		tab.dialogArmed = true
		tab.dialogAccept = accept
		tab.dialogPrompt = text(1)
		tab.mu.Unlock()
		return true, nil
	case "dialogText":
		tab.mu.Lock()
		msg := tab.dialogMessage
		tab.mu.Unlock()
		return msg, nil
	case "intercept":
		if len(args) == 0 {
			tab.mu.Lock()
			tab.rules = nil
			tab.mu.Unlock()
			return true, nil
		}
		if selector == "" {
			return nil, errors.New("intercept requires a URL substring")
		}
		body, status, err := interceptBody(args)
		if err != nil {
			return nil, err
		}
		if err := tab.enableFetch(ctx); err != nil {
			return nil, err
		}
		tab.mu.Lock()
		replaced := false
		for i := range tab.rules {
			if tab.rules[i].pattern == selector {
				tab.rules[i] = intercept{pattern: selector, status: status, body: body}
				replaced = true
			}
		}
		if !replaced {
			tab.rules = append(tab.rules, intercept{pattern: selector, status: status, body: body})
		}
		tab.mu.Unlock()
		return true, nil
	case "window":
		if len(args) == 0 {
			return p.windows(ctx)
		}
		if n, ok := value.AsInt(args[0]); ok {
			return true, p.useWindow(ctx, n)
		}
		if err := p.openWindow(ctx, text(0)); err != nil {
			return nil, err
		}
		return true, nil
	default:
		return nil, fmt.Errorf("driver action %q is not supported", name)
	}
}
