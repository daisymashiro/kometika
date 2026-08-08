// Command droplink is the CLI front-end for the droplink package.
//
// Usage:
//
//	droplink -code YIdmJhIK              # fast mode (default)
//	droplink -code YIdmJhIK -mode natural
//	droplink                            # interactive prompt
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"droplink"
)

func main() {
	modeFlag := flag.String("mode", "fast", "unlock mode: fast or natural")
	codeFlag := flag.String("code", "", "droplink URL or code (interactive prompt if empty)")
	flag.Parse()

	mode := droplink.Mode(strings.ToLower(*modeFlag))
	if mode != droplink.ModeFast && mode != droplink.ModeNatural {
		fmt.Fprintln(os.Stderr, "mode must be fast or natural")
		os.Exit(2)
	}

	raw := *codeFlag
	if raw == "" {
		fmt.Print("Paste droplink link or code: ")
		raw, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	code, err := droplink.ParseCode(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("→ code: %s\n\n", code)

	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  Droplink Wheel Unlocker — %s\n", strings.ToUpper(string(mode)))
	fmt.Println(strings.Repeat("=", 60))

	t0 := time.Now()
	res, err := droplink.Unlock(context.Background(), code, droplink.Options{
		Mode: mode,
		Logf: func(format string, args ...any) {
			fmt.Printf("[t=%6.1fs] %s\n", time.Since(t0).Seconds(), fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "unlock failed:", err)
		os.Exit(1)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	if res.URL != "" {
		fmt.Printf("✅ FINAL LINK  (t = %.1fs)\n", res.Elapsed.Seconds())
		fmt.Printf("   %s\n", res.URL)
		if res.Title != "" {
			fmt.Printf("   TeraBox: %s\n", res.Title)
		}
		if res.WithEarn {
			fmt.Println("   (gate: Go With earning)")
		}
	} else {
		fmt.Println("⚠️  Link not extracted. Full gate response:")
		if res.RawResp != "" {
			fmt.Printf("   %s\n", res.RawResp)
		} else {
			fmt.Println("   no /links/ response seen")
		}
		fmt.Println("   Tip: retry with -mode natural, or the wheel may differ.")
	}
	fmt.Println(strings.Repeat("=", 60))
}
