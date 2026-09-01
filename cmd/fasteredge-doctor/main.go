// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FasterEdge/FasterEdgeDoctor"
)

func main() {
	mode := flag.String("mode", "local", "check mode: local, remote, or onekey")
	root := flag.String("root", "", "FasterEdge repository root (default: parent of current directory)")
	endpoint := flag.String("endpoint", "", "remote FasterEdge doctor endpoint, e.g. https://edge:7000")
	tokenFile := flag.String("token", "", "JSON OneKey token file for onekey mode")
	listen := flag.String("listen", ":7080", "listen address for serve mode")
	secretFile := flag.String("secret-file", "", "OneKey shared secret file for authenticated serve mode")
	requireOneKey := flag.Bool("require-onekey", false, "require OneKey authentication in serve mode")
	jsonOut := flag.Bool("json", false, "print machine-readable JSON")
	timeout := flag.Duration("timeout", 30*time.Second, "overall check timeout")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var report doctor.Report
	var err error
	switch *mode {
	case "local":
		if *root == "" {
			wd, _ := os.Getwd()
			*root = wd
			if filepath.Base(*root) == "FasterEdgeDoctor" {
				*root = filepath.Dir(*root)
			}
		}
		report = doctor.Local(ctx, *root)
	case "serve":
		if *root == "" {
			wd, _ := os.Getwd()
			*root = wd
			if filepath.Base(*root) == "FasterEdgeDoctor" {
				*root = filepath.Dir(*root)
			}
		}
		secret := ""
		if *secretFile != "" {
			b, e := os.ReadFile(*secretFile)
			if e != nil {
				fail(e.Error())
			}
			secret = strings.TrimSpace(string(b))
		}
		if *requireOneKey && secret == "" {
			fail("-secret-file is required with -require-onekey")
		}
		server := &http.Server{Addr: *listen, Handler: &doctor.Server{Root: *root, Secret: secret, RequireOneKey: *requireOneKey, Timeout: *timeout}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: *timeout + 5*time.Second, IdleTimeout: 60 * time.Second}
		fmt.Fprintf(os.Stderr, "FasterEdgeDoctor listening on %s\n", *listen)
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			fail(e.Error())
		}
		return
	case "remote", "onekey":
		if *endpoint == "" {
			fail("-endpoint is required for remote and onekey modes")
		}
		client := doctor.Client{Endpoint: *endpoint}
		if *mode == "onekey" {
			if *tokenFile == "" {
				fail("-token is required for onekey mode")
			}
			b, e := os.ReadFile(*tokenFile)
			if e != nil {
				fail(e.Error())
			}
			var t doctor.Token
			if e = json.Unmarshal(b, &t); e != nil {
				fail("invalid token JSON: " + e.Error())
			}
			client.Token = &t
		}
		report, err = client.Check(ctx, *mode == "onekey")
		if err != nil {
			report.OK = false
			report.Checks = append(report.Checks, doctor.Check{Name: "remote-request", Status: doctor.Fail, Detail: err.Error()})
		}
	default:
		fail("unknown -mode: " + *mode)
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
	} else {
		printReport(report)
	}
	if !report.OK {
		os.Exit(1)
	}
}
func printReport(r doctor.Report) {
	fmt.Printf("FasterEdgeDoctor: %s [%s]\n", r.Target, status(r.OK))
	for _, c := range r.Checks {
		fmt.Printf("%-28s %-5s %s\n", c.Name, c.Status, c.Detail)
	}
}
func status(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAIL"
}
func fail(msg string) { fmt.Fprintln(os.Stderr, "error:", msg); os.Exit(2) }
