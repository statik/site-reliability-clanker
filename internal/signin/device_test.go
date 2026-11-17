package signin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/signin/fakeidp"
)

// program is a client with no browser: it has no cookie.
type program struct {
	t    *testing.T
	site *site
}

func (p program) post(path string, form url.Values, headers map[string]string) (int, map[string]any, string) {
	p.t.Helper()
	request, err := http.NewRequest(http.MethodPost, p.site.url+path, strings.NewReader(form.Encode()))
	if err != nil {
		p.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		p.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		p.t.Fatal(err)
	}
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body) // a page is not JSON; the caller then reads the text
	return response.StatusCode, body, string(raw)
}

// start asks for a code and returns the device code and the user code.
func (p program) start() (device, code string) {
	p.t.Helper()
	status, body, _ := p.post("/auth/device/code", nil, nil)
	if status != http.StatusOK {
		p.t.Fatalf("the request for a code answered %d", status)
	}
	device, _ = body["device_code"].(string)
	code, _ = body["user_code"].(string)
	want := p.site.url + "/auth/device?user_code=" + code
	if device == "" || len(code) != 9 || body["verification_uri_complete"] != want {
		p.t.Fatalf("the answer to the request for a code is not complete: %v", body)
	}
	return device, code
}

func (p program) token(device string) (int, map[string]any) {
	p.t.Helper()
	status, body, _ := p.post("/auth/device/token", url.Values{"device_code": {device}}, nil)
	return status, body
}

// who asks the test page who the request is, with the given Authorization header.
func (p program) who(authorization string) (int, string) {
	p.t.Helper()
	request, err := http.NewRequest(http.MethodGet, p.site.url+"/who", nil)
	if err != nil {
		p.t.Fatal(err)
	}
	request.Header.Set("Authorization", authorization)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		p.t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, strings.TrimSpace(string(body))
}

// signedIn returns a site where alice is signed in in the browser.
func signedIn(t *testing.T) (*site, program) {
	t.Helper()
	s := newSite(t)
	s.provider.SignInAs(fakeidp.Person{Email: "alice@example.com", Verified: true})
	if status, _ := s.get(t, "/auth/login"); status != http.StatusOK {
		t.Fatalf("sign-in answered %d", status)
	}
	return s, program{t: t, site: s}
}

func (s *site) decide(t *testing.T, code, decision string) (int, string) {
	t.Helper()
	response, err := s.browser.PostForm(s.url+"/auth/device", url.Values{"user_code": {code},
		"decision": {decision}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func TestAProgramGetsATokenWhenASignedInPersonApprovesItsCode(t *testing.T) {
	s, p := signedIn(t)
	device, code := p.start()
	if status, body := p.token(device); status != http.StatusBadRequest || body["error"] != "authorization_pending" {
		t.Fatalf("before the approval: %d %v", status, body)
	}
	// The person can type the code in lower case and with no dash.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	if status, page := s.get(t, "/auth/device?user_code="+typed); status != http.StatusOK ||
		!strings.Contains(page, code) || !strings.Contains(page, "alice@example.com") {
		t.Fatalf("the page for the code: %d %s", status, page)
	}
	if status, page := s.decide(t, code, "approve"); status != http.StatusOK ||
		!strings.Contains(page, "signed in as alice@example.com") {
		t.Fatalf("the approval: %d %s", status, page)
	}
	status, body := p.token(device)
	token, _ := body["access_token"].(string)
	if status != http.StatusOK || token == "" || body["token_type"] != "Bearer" {
		t.Fatalf("after the approval: %d %v", status, body)
	}
	if status, text := p.who("Bearer " + token); status != http.StatusOK || text != "alice@example.com at /who" {
		t.Fatalf("the token as a bearer token: %d %q", status, text)
	}
	// git sends the token as the password of basic authentication.
	request, _ := http.NewRequest(http.MethodGet, s.url+"/who", nil)
	request.SetBasicAuth("anything", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the token as a password answered %d", response.StatusCode)
	}
	if status, body := p.token(device); status != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Fatalf("a device code gave a second answer: %d %v", status, body)
	}
	if status, _ := p.who("Bearer " + token + "x"); status != http.StatusUnauthorized {
		t.Fatalf("a wrong token answered %d", status)
	}
}

func TestADeniedCodeGivesNoToken(t *testing.T) {
	s, p := signedIn(t)
	device, code := p.start()
	if status, page := s.decide(t, code, "deny"); status != http.StatusOK || !strings.Contains(page, "refused") {
		t.Fatalf("the denial: %d %s", status, page)
	}
	if status, body := p.token(device); status != http.StatusBadRequest || body["error"] != "access_denied" {
		t.Fatalf("after the denial: %d %v", status, body)
	}
}

func TestOnlyAPersonInABrowserDecidesOnACode(t *testing.T) {
	s, p := signedIn(t)
	device, code := p.start()
	if status, _ := s.decide(t, code, "approve"); status != http.StatusOK {
		t.Fatalf("the approval answered %d", status)
	}
	_, body := p.token(device)
	token, _ := body["access_token"].(string)

	// A program with a token cannot approve a second code and get more tokens.
	_, second := p.start()
	form := url.Values{"user_code": {second}, "decision": {"approve"}}
	if status, _, _ := p.post("/auth/device", form, map[string]string{"Authorization": "Bearer " + token}); status != http.StatusUnauthorized {
		t.Fatalf("a program approved a code: %d", status)
	}
	// Nobody decides without a sign-in.
	if status, _, _ := p.post("/auth/device", form, nil); status != http.StatusUnauthorized {
		t.Fatalf("a request with no sign-in decided on a code: %d", status)
	}
	// A page of another site cannot decide with the session of the person.
	request, _ := http.NewRequest(http.MethodPost, s.url+"/auth/device", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.example")
	response, err := s.browser.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("a request from another site answered %d", response.StatusCode)
	}
	// A person with no sign-in who opens the page goes to the sign-in first.
	plain := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err = plain.Get(s.url + "/auth/device?user_code=" + second)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound ||
		!strings.HasPrefix(response.Header.Get("Location"), "/auth/login?return=") {
		t.Fatalf("the page with no sign-in: %d %s", response.StatusCode, response.Header.Get("Location"))
	}
}

func TestCodesAndTokensEnd(t *testing.T) {
	s, p := signedIn(t)
	device, code := p.start()
	s.service.now = func() time.Time { return time.Now().Add(deviceLife + time.Minute) }
	if status, _ := s.get(t, "/auth/device?user_code="+code); status != http.StatusNotFound {
		t.Fatalf("the page for an old code answered %d", status)
	}
	if status, _ := s.decide(t, code, "approve"); status != http.StatusNotFound {
		t.Fatalf("the approval of an old code answered %d", status)
	}
	if status, body := p.token(device); status != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Fatalf("an old device code: %d %v", status, body)
	}
	s.service.now = time.Now
	if status, body := p.token("never-issued"); status != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Fatalf("a device code that does not exist: %d %v", status, body)
	}

	device, code = p.start()
	if status, _ := s.decide(t, code, "approve"); status != http.StatusOK {
		t.Fatalf("the approval answered %d", status)
	}
	_, body := p.token(device)
	token, _ := body["access_token"].(string)
	s.service.now = func() time.Time { return time.Now().Add(tokenLife + time.Minute) }
	if status, _ := p.who("Bearer " + token); status != http.StatusUnauthorized {
		t.Fatalf("a token after its life answered %d", status)
	}
}

func TestTheCodeAPersonTypesIsBroughtToOneForm(t *testing.T) {
	for typed, want := range map[string]string{"bcdf-ghjk": "BCDF-GHJK", "BCDFGHJK": "BCDF-GHJK",
		" bcdf ghjk ": "BCDF-GHJK", "short": "", "": "", "BCDF-GHJK-L": ""} {
		if got := normalCode(typed); got != want {
			t.Errorf("normalCode(%q) = %q, want %q", typed, got, want)
		}
	}
}
