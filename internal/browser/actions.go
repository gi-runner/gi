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

func runAction[T any](ctx context.Context, p *page, action chromedp.Action[T]) (out T, err error) {
	err = p.act(ctx, func(c context.Context) error { var runErr error; out, runErr = chromedp.Run(c, action); return runErr })
	return out, err
}

// Locators are quoted as data, never spliced into browser JavaScript as code.
func nodeExpression(selector string) string {
	if strings.HasPrefix(selector, "/") || strings.HasPrefix(selector, "(") {
		return "document.evaluate(" + strconv.Quote(selector) + ",document,null,XPathResult.FIRST_ORDERED_NODE_TYPE,null).singleNodeValue"
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
			return "Array.from(document.querySelectorAll(" + strconv.Quote(tag) + ")).find(function(e){return e.textContent.trim()" + comparison + "})"
		}
	}
	return "document.querySelector(" + strconv.Quote(selector) + ")"
}

func (p *page) Action(ctx context.Context, name string, args []any) (any, error) {
	text := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return value.Sprint(args[i])
	}
	selector := text(0)
	node := nodeExpression(selector)
	eval := func(src string) (any, error) { return runAction(ctx, p, chromedp.Evaluate[any](src)) }
	wait := func(src string) (any, error) {
		return runAction(ctx, p, chromedp.Poll[bool](src, chromedp.WithPollingInterval(50*time.Millisecond)))
	}
	element := func(code string) (any, error) {
		return eval("(function(e){if(!e)throw Error('element not found: '+" + strconv.Quote(selector) + ");" + code + "})(" + node + ")")
	}
	switch name {
	case "getUrl":
		return runAction(ctx, p, chromedp.Location())
	case "title":
		return runAction(ctx, p, chromedp.Title())
	case "back":
		return runAction(ctx, p, chromedp.NavigateBack())
	case "forward":
		return runAction(ctx, p, chromedp.NavigateForward())
	case "refresh":
		return runAction(ctx, p, chromedp.Reload())
	case "screenshot":
		return runAction(ctx, p, chromedp.FullScreenshot(100))
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
		if strings.HasPrefix(selector, "{") {
			return element("e.click();return true")
		}
		return runAction(ctx, p, chromedp.Click(selector))
	case "input":
		if len(args) < 2 {
			return nil, errors.New("input requires a locator and text")
		}
		return runAction(ctx, p, chromedp.SendKeys(selector, text(1)))
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
		list := "Array.from(document.querySelectorAll(" + strconv.Quote(selector) + "))"
		if strings.HasPrefix(selector, "/") {
			list = "(function(){var r=document.evaluate(" + strconv.Quote(selector) + ",document,null,XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,null);var a=[];for(var i=0;i<r.snapshotLength;i++)a.push(r.snapshotItem(i));return a})()"
		}
		return eval(list + ".map(function(n){var p='';for(;n&&n.nodeType===1;n=n.parentElement){var i=1;for(var s=n.previousElementSibling;s;s=s.previousElementSibling)if(s.tagName===n.tagName)i++;p='/'+n.tagName.toLowerCase()+'['+i+']'+p}return p})")
	default:
		return nil, fmt.Errorf("driver action %q is not supported", name)
	}
}
