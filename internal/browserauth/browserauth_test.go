package browserauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeBrowser simulates the browser: it fetches the page, then POSTs the
// captured tokens to /callback, exactly like the embedded JS would.
func fakeBrowser(t *testing.T, payload string) func(string) error {
	return func(pageURL string) error {
		base := strings.TrimSuffix(pageURL, "/")
		go func() {
			resp, err := http.Get(pageURL)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			r, err := http.Post(base+"/callback", "application/json", strings.NewReader(payload))
			if err != nil {
				t.Errorf("callback POST: %v", err)
				return
			}
			_ = r.Body.Close()
		}()
		return nil
	}
}

func TestFlow_Run_ReturnsCallbackResult(t *testing.T) {
	payload := `{"idToken":"idt","refreshToken":"rft","uid":"u1","email":"a@b.c","expiresIn":3600}`
	f := Flow{
		APIKey: "k", AuthDomain: "d", Project: "p",
		OpenBrowser: fakeBrowser(t, payload),
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IDToken != "idt" || res.RefreshToken != "rft" || res.UID != "u1" || res.Email != "a@b.c" {
		t.Fatalf("bad result: %+v", res)
	}
	if res.ExpiresIn != time.Hour {
		t.Fatalf("expiresIn = %v", res.ExpiresIn)
	}
}

func TestFlow_Run_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := Flow{OpenBrowser: func(string) error { cancel(); return nil }}
	if _, err := f.Run(ctx); err == nil {
		t.Fatalf("expected error on context cancel")
	}
}

func TestFlow_Run_OpenBrowserError(t *testing.T) {
	f := Flow{OpenBrowser: func(string) error { return io.EOF }}
	if _, err := f.Run(context.Background()); err == nil {
		t.Fatalf("expected error when OpenBrowser fails")
	}
}

func TestFlow_Run_IgnoresBadCallbackThenSucceeds(t *testing.T) {
	good := `{"idToken":"idt","refreshToken":"rft","uid":"u1","email":"a@b.c","expiresIn":3600}`
	open := func(pageURL string) error {
		base := strings.TrimSuffix(pageURL, "/")
		go func() {
			// First a malformed body (400, no result), then a valid one.
			if r, err := http.Post(base+"/callback", "application/json", strings.NewReader("{bad")); err == nil {
				_ = r.Body.Close()
			}
			if r, err := http.Post(base+"/callback", "application/json", strings.NewReader(good)); err == nil {
				_ = r.Body.Close()
			}
		}()
		return nil
	}
	res, err := Flow{OpenBrowser: open}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IDToken != "idt" {
		t.Fatalf("idToken = %q", res.IDToken)
	}
}

func TestFlow_Run_NilOpenBrowser(t *testing.T) {
	f := Flow{APIKey: "k"}
	if _, err := f.Run(context.Background()); err == nil {
		t.Fatalf("expected error when OpenBrowser is nil")
	}
}

func TestRenderPage_InjectsConfig(t *testing.T) {
	f := Flow{APIKey: "mykey", AuthDomain: "dom", Project: "proj", AuthEmulatorHost: "localhost:9099"}
	page, err := f.renderPage()
	if err != nil {
		t.Fatalf("renderPage: %v", err)
	}
	s := string(page)
	for _, want := range []string{"mykey", "dom", "proj", "localhost:9099"} {
		if !strings.Contains(s, want) {
			t.Fatalf("page missing %q", want)
		}
	}
	if strings.Contains(s, "__SNEAT_CONFIG__") {
		t.Fatalf("placeholder not replaced")
	}
}
