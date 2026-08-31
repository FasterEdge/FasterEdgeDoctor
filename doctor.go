// FasterEdge 开源项目 · https://github.com/FasterEdge · https://gitee.com/FasterEdge
// Package doctor provides repository and remote health diagnostics for FasterEdge.
package doctor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Warn Status = "warn"
	Skip Status = "skip"
)

type Check struct {
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Detail   string        `json:"detail,omitempty"`
	Duration time.Duration `json:"duration_ns,omitempty"`
}
type Report struct {
	Target     string    `json:"target"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Checks     []Check   `json:"checks"`
	OK         bool      `json:"ok"`
}
type Variant struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Path       string   `json:"path"`
	Toolchains []string `json:"toolchains,omitempty"`
}

var knownToolchains = map[string]bool{"arduino": true, "platformio_ide": true, "keil": true, "mounriver": true, "mplab_x": true, "pm_ide": true, "vivado": true, "micro_blaze": true, "vitis_hls": true, "symbi_flow": true}

// Discover finds every immediate MCU-* and FPGA-* implementation.
func Discover(root string) ([]Variant, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Variant
	for _, e := range entries {
		if !e.IsDir() || !(strings.HasPrefix(e.Name(), "MCU-") || strings.HasPrefix(e.Name(), "FPGA-")) {
			continue
		}
		kind := "MCU"
		if strings.HasPrefix(e.Name(), "FPGA-") {
			kind = "FPGA"
		}
		v := Variant{Name: e.Name(), Kind: kind, Path: filepath.Join(root, e.Name())}
		children, _ := os.ReadDir(v.Path)
		for _, c := range children {
			if c.IsDir() && knownToolchains[c.Name()] {
				v.Toolchains = append(v.Toolchains, c.Name())
			}
		}
		sort.Strings(v.Toolchains)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Local performs repository layout checks and the main Go test suite.
func Local(ctx context.Context, root string) Report {
	r := Report{Target: root, StartedAt: time.Now()}
	add := func(n string, s Status, d string) { r.Checks = append(r.Checks, Check{Name: n, Status: s, Detail: d}) }
	main := filepath.Join(root, "FasterEdge")
	if st, e := os.Stat(main); e != nil || !st.IsDir() {
		add("main-repository", Fail, "FasterEdge directory is missing")
	} else {
		add("main-repository", Pass, "FasterEdge directory found")
	}
	vs, e := Discover(root)
	if e != nil {
		add("variant-discovery", Fail, e.Error())
	} else if len(vs) == 0 {
		add("variant-discovery", Fail, "no MCU-* or FPGA-* variants found")
	} else {
		add("variant-discovery", Pass, fmt.Sprintf("discovered %d variants", len(vs)))
		for _, v := range vs {
			checkVariant(v, add)
		}
	}
	if _, e := os.Stat(filepath.Join(main, "go.mod")); e != nil {
		add("go-module", Fail, "FasterEdge/go.mod missing")
	} else if _, e := exec.LookPath("go"); e != nil {
		add("go-test", Warn, "go executable not found")
	} else if ctx.Err() != nil {
		add("go-test", Skip, ctx.Err().Error())
	} else {
		cmd := exec.CommandContext(ctx, "go", "test", "./...")
		cmd.Dir = main
		if out, e := cmd.CombinedOutput(); e != nil {
			add("go-test", Fail, compact(out))
		} else {
			add("go-test", Pass, "go test ./... passed")
		}
	}
	finish(&r)
	return r
}
func checkVariant(v Variant, add func(string, Status, string)) {
	mustFile(filepath.Join(v.Path, "README.md"), v.Name+"/README", add)
	mustFile(filepath.Join(v.Path, "LICENSE"), v.Name+"/LICENSE", add)
	if len(v.Toolchains) == 0 {
		add(v.Name+"/toolchains", Fail, "no recognized implementation directory")
	} else {
		add(v.Name+"/toolchains", Pass, strings.Join(v.Toolchains, ", "))
	}
	for _, tc := range v.Toolchains {
		b := filepath.Join(v.Path, tc)
		switch tc {
		case "arduino", "platformio_ide":
			mustFile(filepath.Join(b, "platformio.ini"), v.Name+"/"+tc+"/platformio.ini", add)
			if !hasAny(b, "src/main.c", "src/main.cpp", "src/main.asm") {
				add(v.Name+"/"+tc+"/entry", Fail, "no src/main.c, main.cpp, or main.asm")
			}
		case "keil":
			if !hasGlob(filepath.Join(b, "MDK-ARM", "*.uvproj*")) {
				add(v.Name+"/keil/project", Fail, "no MDK-ARM project")
			}
		case "mounriver":
			mustFile(filepath.Join(b, ".project"), v.Name+"/mounriver/.project", add)
		case "vivado":
			if !hasGlob(filepath.Join(b, "rtl", "*.v")) && !hasGlob(filepath.Join(b, "rtl", "*.sv")) {
				add(v.Name+"/vivado/rtl", Fail, "no RTL source")
			}
			mustFile(filepath.Join(b, "scripts", "create_project.tcl"), v.Name+"/vivado/create_project.tcl", add)
			if !hasGlob(filepath.Join(b, "xdc", "*.xdc")) {
				add(v.Name+"/vivado/xdc", Fail, "no XDC constraints")
			}
		case "micro_blaze":
			mustFile(filepath.Join(b, "Makefile"), v.Name+"/micro_blaze/Makefile", add)
			mustFile(filepath.Join(b, "lscript.ld"), v.Name+"/micro_blaze/lscript.ld", add)
		case "vitis_hls":
			if !hasGlob(filepath.Join(b, "src", "*.cpp")) {
				add(v.Name+"/vitis_hls/src", Fail, "no HLS C++ source")
			}
		case "symbi_flow":
			mustFile(filepath.Join(b, "scripts", "build.sh"), v.Name+"/symbi_flow/build.sh", add)
		case "mplab_x", "pm_ide":
			if !hasGlob(filepath.Join(b, "src", "*")) {
				add(v.Name+"/"+tc+"/src", Fail, "no source files")
			}
		}
	}
}
func mustFile(p, n string, add func(string, Status, string)) {
	if st, e := os.Stat(p); e != nil || st.IsDir() {
		add(n, Fail, "missing required file")
	} else {
		add(n, Pass, "present")
	}
}
func hasAny(b string, ns ...string) bool {
	for _, n := range ns {
		if st, e := os.Stat(filepath.Join(b, n)); e == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
func hasGlob(p string) bool { m, _ := filepath.Glob(p); return len(m) > 0 }
func compact(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 4096 {
		return s[len(s)-4096:]
	}
	return s
}
func finish(r *Report) {
	r.FinishedAt = time.Now()
	r.OK = true
	for _, c := range r.Checks {
		if c.Status == Fail {
			r.OK = false
		}
	}
}

type Token struct {
	Subject   string    `json:"subject"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Signature string    `json:"signature"`
}
type Client struct {
	HTTP     *http.Client
	Endpoint string
	Token    *Token
}

// Check calls the dedicated read-only endpoint; it never exposes generic commands.
func (c Client) Check(ctx context.Context, oneKey bool) (Report, error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	u, e := url.Parse(strings.TrimRight(c.Endpoint, "/") + "/v1/check")
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Report{}, errors.New("invalid endpoint")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(`{"scope":"health"}`))
	if e != nil {
		return Report{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	if oneKey {
		if c.Token == nil {
			return Report{}, errors.New("onekey token is required")
		}
		b, _ := json.Marshal(c.Token)
		req.Header.Set("Authorization", "OneKey "+base64.RawURLEncoding.EncodeToString(b))
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return Report{}, e
	}
	defer resp.Body.Close()
	var r Report
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); e != nil {
		return Report{}, fmt.Errorf("decode health response: %w", e)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return r, fmt.Errorf("remote health returned %s", resp.Status)
	}
	return r, nil
}

// Server exposes a narrow health endpoint. Secret enables OneKey compatibility authentication.
type Server struct {
	Root          string
	Secret        string
	RequireOneKey bool
	Timeout       time.Duration
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/check" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RequireOneKey || r.Header.Get("Authorization") != "" {
		if !s.authenticate(r.Header.Get("Authorization")) {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
	}
	defer r.Body.Close()
	var body struct {
		Scope string `json:"scope"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&body); e != nil || body.Scope != "health" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	t := s.Timeout
	if t <= 0 {
		t = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), t)
	defer cancel()
	report := Local(ctx, s.Root)
	w.Header().Set("Content-Type", "application/json")
	if !report.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(report)
}
func (s *Server) authenticate(h string) bool {
	if s.Secret == "" || !strings.HasPrefix(h, "OneKey ") {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(h, "OneKey "))
	if e != nil {
		return false
	}
	var t Token
	if json.Unmarshal(b, &t) != nil {
		return false
	}
	now := time.Now()
	if t.Subject == "" || t.IssuedAt.IsZero() || t.ExpiresAt.Before(now) || !t.ExpiresAt.After(t.IssuedAt) || t.IssuedAt.After(now.Add(time.Minute)) {
		return false
	}
	return VerifyToken(s.Secret, t)
}
func SignToken(secret string, t Token) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%d|%d", t.Subject, t.IssuedAt.UnixNano(), t.ExpiresAt.UnixNano())
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func VerifyToken(secret string, t Token) bool {
	got, e := base64.RawURLEncoding.DecodeString(t.Signature)
	if e != nil {
		return false
	}
	want, _ := base64.RawURLEncoding.DecodeString(SignToken(secret, t))
	return hmac.Equal(got, want)
}
