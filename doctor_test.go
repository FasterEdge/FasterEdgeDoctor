package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(p, s string) {
		p = filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	must("FasterEdge/go.mod", "module test\n\ngo 1.25\n")
	must("FasterEdge/test.go", "package test\n")
	must("MCU-Test/README.md", "test")
	must("MCU-Test/LICENSE", "test")
	must("MCU-Test/platformio_ide/platformio.ini", "[env:test]\nplatform=x\nboard=y\n")
	must("MCU-Test/platformio_ide/src/main.c", "int main(void){}")
	return root
}
func TestDiscover(t *testing.T) {
	root := fixture(t)
	vs, err := Discover(root)
	if err != nil || len(vs) != 1 || vs[0].Name != "MCU-Test" {
		t.Fatalf("%+v %v", vs, err)
	}
}
func TestLocalFixture(t *testing.T) {
	r := Local(context.Background(), fixture(t))
	if !r.OK {
		t.Fatalf("report: %+v", r)
	}
}
func TestTokenCompatibility(t *testing.T) {
	now := time.Now().Round(0)
	tok := Token{Subject: "edge-1", IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	tok.Signature = SignToken("0123456789abcdef", tok)
	if !VerifyToken("0123456789abcdef", tok) {
		t.Fatal("token should verify")
	}
	tok.Subject = "other"
	if VerifyToken("0123456789abcdef", tok) {
		t.Fatal("tampered token verified")
	}
}
func TestRemoteAndOneKey(t *testing.T) {
	root := fixture(t)
	secret := "0123456789abcdef"
	srv := httptest.NewServer(&Server{Root: root, Secret: secret, RequireOneKey: true})
	defer srv.Close()
	now := time.Now()
	tok := Token{Subject: "edge", IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	tok.Signature = SignToken(secret, tok)
	r, err := (Client{Endpoint: srv.URL, Token: &tok}).Check(context.Background(), true)
	if err != nil || !r.OK {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestServerRejectsBadAuthAndRequest(t *testing.T) {
	s := httptest.NewServer(&Server{Root: fixture(t), Secret: "0123456789abcdef", RequireOneKey: true})
	defer s.Close()
	req, _ := http.NewRequest(http.MethodPost, s.URL+"/v1/check", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
func TestReportJSON(t *testing.T) {
	b, err := json.Marshal(Local(context.Background(), fixture(t)))
	if err != nil || len(b) == 0 {
		t.Fatal(err)
	}
}
