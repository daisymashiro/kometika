// Package droplink unlocks droplink.co ad-wall shortener links in headless
// Chromium (playwright-go) and returns the final destination URL.
//
// Port of the droplink_fast.py / droplink_natural.py scripts. The wheel:
//
//	droplink.co/CODE -> tech8s.net wall A -> game5s.com wall C -> gate
//	droplink.co/CODE (auto POST /links/go) -> JSON {"url": "..."}
//
// Call Unlock with a droplink URL or bare code. See cmd/droplink for the CLI.
package droplink

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Mode selects how the walls are walked.
type Mode string

const (
	// ModeFast fast-forwards all >=1s JS timers (countdowns finish instantly),
	// skips wall B and jumps straight to the game5s safe2 hop. ~20-45s.
	ModeFast Mode = "fast"
	// ModeNatural waits real seconds on every countdown and auto-detects the
	// wall template. ~90-110s, survives wheel changes.
	ModeNatural Mode = "natural"
)

// Options configures an Unlock call.
type Options struct {
	Mode Mode
	// Logf receives progress lines; nil means silent.
	Logf func(format string, args ...any)
}

// Result is what Unlock extracted.
type Result struct {
	URL      string // final destination (e.g. 1024terabox.com/s/...)
	Title    string // <title> of the destination page, if fetchable
	RawResp  string // raw JSON from the gate POST /links/go
	WithEarn bool   // gate reported "Go With earning" (residential-ish IP)
	Elapsed  time.Duration
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

// timerOverride makes every >=1s JS timer fire in 1ms, so countdowns finish instantly.
const timerOverride = `() => {
    try {
        const oi = window.setInterval.bind(window);
        const ot = window.setTimeout.bind(window);
        window.setInterval = (fn, ms, ...a) => oi(fn, ms >= 1000 ? 1 : ms, ...a);
        window.setTimeout  = (fn, ms, ...a) => ot(fn, ms >= 1000 ? 1 : ms, ...a);
    } catch (e) {}
}`

// hideJS/showJS emulate switching to the ad tab: headless never fires real
// visibilitychange events, and the countdown gates only check document.hidden.
const (
	hideJS = `() => { Object.defineProperty(document,'hidden',{configurable:true,get:()=>true}); document.dispatchEvent(new Event('visibilitychange')); }`
	showJS = `() => { Object.defineProperty(document,'hidden',{configurable:true,get:()=>false}); document.dispatchEvent(new Event('visibilitychange')); }`
)

var (
	codeRe  = regexp.MustCompile(`droplink\.co/([A-Za-z0-9]+)`)
	bareRe  = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	urlRe   = regexp.MustCompile(`"url"\s*:\s*"([^"]+)"`)
	titleRe = regexp.MustCompile(`(?i)<title>([^<]*)</title>`)
)

// ParseCode extracts the code from 'https://droplink.co/CODE', 'droplink.co/CODE'
// or a bare 'CODE'.
func ParseCode(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if m := codeRe.FindStringSubmatch(raw); m != nil {
		return m[1], nil
	}
	if bareRe.MatchString(raw) {
		return raw, nil
	}
	return "", fmt.Errorf("invalid input. Use e.g. https://droplink.co/CODE or just CODE")
}

func teraboxTitle(ctx context.Context, url string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	m := titleRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type unlocker struct {
	page playwright.Page
	t0   time.Time
	log  func(string, ...any)
}

func (u *unlocker) logf(format string, args ...any) {
	if u.log != nil {
		u.log(format, args...)
	}
}

func clickByText(page playwright.Page, text string, timeout time.Duration) error {
	return page.Locator("button", playwright.PageLocatorOptions{HasText: text}).First().Click(
		playwright.LocatorClickOptions{Timeout: playwright.Float(float64(timeout.Milliseconds()))})
}

func clickByID(page playwright.Page, id string, timeout time.Duration) error {
	return page.Locator("#" + id).Click(
		playwright.LocatorClickOptions{Timeout: playwright.Float(float64(timeout.Milliseconds()))})
}

func pageText(page playwright.Page, n int) string {
	v, err := page.Evaluate("document.body.innerText")
	if err != nil || v == nil {
		return "?"
	}
	return truncate(strings.ReplaceAll(fmt.Sprint(v), "\n", " | "), n)
}

// waitNav waits until the URL changes, then for domcontentloaded.
func waitNav(ctx context.Context, page playwright.Page, timeout time.Duration) {
	old := page.URL()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return
		}
		if page.URL() != old {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateDomcontentloaded,
		Timeout: playwright.Float(15000),
	})
}

func (u *unlocker) countdownStep(seconds int) {
	_, _ = u.page.Evaluate(hideJS)
	time.Sleep(time.Duration(seconds+1) * time.Second)
	_, _ = u.page.Evaluate(showJS)
	time.Sleep(time.Second)
}

// wallAB: template A/C — Step buttons + clickHereContinue modal.
func (u *unlocker) wallAB(labels []string, seconds int) {
	for _, label := range labels {
		if err := clickByText(u.page, label, 6*time.Second); err != nil {
			u.logf("  step '%s' skipped: %s", label, truncate(err.Error(), 80))
			continue
		}
		time.Sleep(time.Second)
		u.countdownStep(seconds)
		u.logf("  step '%s' completed", label)
	}
	if err := clickByID(u.page, "clickHereContinue", 4*time.Second); err == nil {
		time.Sleep(13 * time.Second) // modal = 8 ticks x 1.5s
		_ = clickByID(u.page, "t_modal_close_x", 4*time.Second)
		u.logf("  continue modal finished")
	}
}

// wallB: template B — #droplink-step-one/two buttons opening iframe modals.
func (u *unlocker) wallB() {
	for _, sel := range []string{"#droplink-step-one", "#droplink-step-two"} {
		if err := u.page.Locator(sel).First().Click(
			playwright.LocatorClickOptions{Timeout: playwright.Float(6000)}); err != nil {
			u.logf("  %s skipped: %s", sel, truncate(err.Error(), 80))
			continue
		}
		time.Sleep(12 * time.Second) // let the iframe ad "play" ~10s
		closeSel := strings.Replace(sel, "#droplink", "#close-droplink", 1)
		_ = clickByID(u.page, strings.TrimPrefix(closeSel, "#"), 3*time.Second)
		time.Sleep(time.Second)
		u.logf("  %s step done", sel)
	}
}

// completeWall detects the wall template, completes it, clicks go_d2.
// Returns false when nothing to click.
func (u *unlocker) completeWall() bool {
	hasB, _ := u.page.Locator("#droplink-step-one").Count()
	hasCC, _ := u.page.Locator("#clickHereContinue").Count()
	n2, _ := u.page.Locator("button", playwright.PageLocatorOptions{HasText: "Step 2"}).Count()

	switch {
	case hasB > 0:
		u.logf("wall B template (iframe modals)")
		u.wallB()
	case hasCC > 0:
		labels := []string{"Step 1"}
		if n2 > 0 {
			labels = append(labels, "Step 2")
		}
		u.wallAB(labels, 11) // wall A=8s, wall C=10s; 11s covers both
	default:
		u.logf("no recognized wall — clicking go_d2 directly if present")
	}

	v, err := u.page.Evaluate(`() => {
		const el = document.getElementById('go_d2');
		return el ? el.tagName : null;
	}`)
	if err != nil || v == nil {
		return false
	}
	u.logf("clicking go_d2 <%s>", fmt.Sprint(v))
	_, _ = u.page.Evaluate("document.getElementById('go_d2').click()")
	return true
}

// waitGate waits up to maxSec for the /links/ response captured on respCh.
func (u *unlocker) waitGate(ctx context.Context, maxSec int, respCh chan string) (string, bool) {
	blocking := 0 * time.Second
	for i := 0; i < maxSec; i++ {
		if ctx.Err() != nil {
			return "", false
		}
		select {
		case r := <-respCh:
			return r, true
		default:
		}
		if !strings.Contains(u.page.URL(), "droplink.co") {
			blocking = 3 * time.Second
			break
		}
		time.Sleep(time.Second)
	}
	select {
	case r := <-respCh:
		return r, true
	case <-time.After(blocking):
		return "", false
	}
}

// fast: timer fast-forward, skip wall B, jump straight to game5s safe2 hop.
func (u *unlocker) fast(ctx context.Context, code string, respCh chan string) string {
	page := u.page
	// wall A: 2 steps (fast)
	for _, label := range []string{"Step 1", "Step 2"} {
		if err := clickByText(page, label, 5*time.Second); err != nil {
			continue
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = page.Evaluate(hideJS)
		time.Sleep(600 * time.Millisecond)
		_, _ = page.Evaluate(showJS)
	}
	_ = clickByID(page, "clickHereContinue", 3*time.Second)
	time.Sleep(600 * time.Millisecond)
	_ = clickByID(page, "t_modal_close_x", 3*time.Second)
	u.logf("wall A done — jumping straight to game5s hop (skip wall B)")

	// jump to game5s (observed wheel invariant)
	_, _ = page.Evaluate(fmt.Sprintf("location.href = 'https://game5s.com/safe2.php?link=%s'", code))
	waitNav(ctx, page, 20*time.Second)
	time.Sleep(4 * time.Second)
	u.logf("game5s: %s | %s", page.URL(), pageText(page, 160))

	// remaining walls: click go_d2 anchors directly
	for hop := 0; hop < 6; hop++ {
		if ctx.Err() != nil {
			break
		}
		v, err := page.Evaluate("!!document.getElementById('go_d2')")
		if err != nil || v == nil {
			break
		}
		ok, _ := v.(bool)
		if !ok {
			break
		}
		u.logf("hop %d: click go_d2", hop)
		_, _ = page.Evaluate("document.getElementById('go_d2').click()")
		waitNav(ctx, page, 20*time.Second)
		time.Sleep(2 * time.Second)
		u.logf("landed: %s", page.URL())
	}
	resp, _ := u.waitGate(ctx, 15, respCh)
	return resp
}

// natural: full wheel, template auto-detect, real waits.
func (u *unlocker) natural(ctx context.Context, respCh chan string) string {
	for hop := 0; hop < 8; hop++ {
		if ctx.Err() != nil {
			break
		}
		t := pageText(u.page, 180)
		u.logf("[hop %d] %s | %s", hop, u.page.URL(), t)
		if strings.Contains(u.page.URL(), "droplink.co") && strings.Contains(t, "JOIN OUR TELEGRAM") {
			u.logf("GATE reached")
			break
		}
		if !u.completeWall() {
			u.logf("nothing to click — stopping")
			break
		}
		waitNav(ctx, u.page, 30*time.Second)
	}
	resp, _ := u.waitGate(ctx, 20, respCh)
	return resp
}

func extractURL(resp string) string {
	m := urlRe.FindStringSubmatch(resp)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], `\/`, "/")
}

// Unlock walks the droplink wheel for code and returns the final link.
// opts.Mode empty means ModeFast.
func Unlock(ctx context.Context, code string, opts Options) (Result, error) {
	var res Result
	if opts.Mode == "" {
		opts.Mode = ModeFast
	}
	if _, err := ParseCode(code); err != nil {
		return res, err
	}
	start := time.Now()

	pw, err := playwright.Run()
	if err != nil {
		return res, fmt.Errorf("playwright driver: %w (fix: run `playwright install chromium` once)", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		return res, fmt.Errorf("chromium launch: %w", err)
	}
	defer browser.Close()

	ctx2, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 800},
	})
	if err != nil {
		return res, fmt.Errorf("browser context: %w", err)
	}
	page, err := ctx2.NewPage()
	if err != nil {
		return res, fmt.Errorf("new page: %w", err)
	}
	if opts.Mode == ModeFast {
		_ = page.AddInitScript(playwright.Script{Content: playwright.String(timerOverride)})
	}

	u := &unlocker{page: page, t0: start, log: opts.Logf}

	respCh := make(chan string, 4) // matched /links/ responses
	page.On("response", func(res playwright.Response) {
		if strings.Contains(res.URL(), "/links/") {
			if body, err := res.Body(); err == nil {
				msg := string(body)
				u.logf("gate RESP: %s", truncate(msg, 300))
				respCh <- msg
			}
		}
	})

	if _, err := page.Goto(fmt.Sprintf("https://droplink.co/%s", code),
		playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout: playwright.Float(90000)}); err != nil {
		return res, fmt.Errorf("goto droplink.co: %w", err)
	}
	time.Sleep(4 * time.Second)

	var resp string
	switch opts.Mode {
	case ModeFast:
		u.logf("wall A: %s | %s", page.URL(), pageText(page, 160))
		resp = u.fast(ctx, code, respCh)
	default:
		resp = u.natural(ctx, respCh)
	}

	res.Elapsed = time.Since(start)
	res.RawResp = resp
	res.URL = extractURL(resp)
	res.WithEarn = strings.Contains(resp, "With earning")
	if res.URL != "" {
		res.Title = teraboxTitle(ctx, res.URL)
	}
	return res, nil
}
