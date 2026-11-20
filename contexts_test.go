package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chijiajian/zstack-cli-go/pkg/config"
	"gopkg.in/yaml.v3"
)

func writeContexts(t *testing.T, cfg config.ZStackConfig) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func readContexts(t *testing.T, p string) config.ZStackConfig {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var c config.ZStackConfig
	if err = yaml.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReuseSavedContextWithoutDefault(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/zstack/v1/hosts" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "ZStack qa-key:") {
			t.Error("query did not use the saved access key")
		}
		inventory(w)
	}))
	defer server.Close()
	qa := config.Context{Endpoint: strings.TrimPrefix(server.URL, "http://"), AccessKeyID: "qa-key", AccessKeySecret: "qa-secret"}
	path := writeContexts(t, config.ZStackConfig{Contexts: map[string]config.Context{
		"test-qa": qa,
		"other":   {Endpoint: "unused.invalid", SessionUUID: "other-session"},
	}})

	for _, tc := range []struct {
		args []string
		code string
		exit int
	}{
		{[]string{"get", "hosts"}, "authentication_failed", 3},
		{[]string{"config", "current-context", "test-qa"}, "invalid_arguments", 2},
	} {
		_, stderr, err := invokeCLI(t, path, "", append(tc.args, "--error-format", "json")...)
		assertDiagnostic(t, stderr, err, tc.code, tc.exit)
		_, stderr, err = invokeCLI(t, path, "", tc.args...)
		if err == nil || !strings.Contains(stderr, "zstack-cli config use-context <name>") {
			t.Fatalf("%v did not explain how to select a saved context: %s", tc.args, stderr)
		}
		if tc.exit == 3 && (!strings.Contains(stderr, "--context <name>") || strings.Contains(stderr, "login")) {
			t.Fatalf("unexpected guidance for a query with saved contexts: %s", stderr)
		}
	}
	if requests.Load() != 0 || readContexts(t, path).CurrentContext != "" {
		t.Fatal("missing or invalid selection made a request or changed the default")
	}

	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := invokeCLI(t, path, "", args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s\n%s", args, err, out, stderr)
		}
		return strings.TrimSpace(out)
	}
	run("get", "hosts", "--context", "test-qa")
	if readContexts(t, path).CurrentContext != "" {
		t.Fatal("one-command selection changed the default")
	}
	run("config", "use-context", "test-qa")
	if run("config", "current-context") != "test-qa" || requests.Load() != 1 {
		t.Fatal("setting or printing the default context made a request or selected the wrong context")
	}
	run("get", "hosts")
	if saved := readContexts(t, path); requests.Load() != 2 || saved.CurrentContext != "test-qa" || saved.Contexts["test-qa"] != qa {
		t.Fatal("query did not reuse the selected context or changed its saved credentials")
	}
}

func TestContextOverrideRoutesReadsAndWrites(t *testing.T) {
	var wrong, selected atomic.Int32
	var deleting atomic.Bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wrong.Add(1); http.Error(w, "wrong environment", 500) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selected.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "ZStack b-key:") {
			t.Errorf("wrong credentials %s", r.Header.Get("Authorization"))
		}
		if r.Method == "GET" {
			state := "Destroyed"
			if deleting.Load() {
				state = "Stopped"
			}
			inventory(w, map[string]any{"uuid": "id1", "name": "demo", "state": state})
			return
		}
		fmt.Fprint(w, `{"inventory":{"uuid":"id1","name":"demo"}}`)
	}))
	defer b.Close()
	cfg := writeContexts(t, config.ZStackConfig{CurrentContext: "a", Contexts: map[string]config.Context{
		"a": {Endpoint: strings.TrimPrefix(a.URL, "http://"), AuthType: "access-key", AccessKeyID: "a-key", AccessKeySecret: "a-secret"},
		"b": {Endpoint: strings.TrimPrefix(b.URL, "http://"), AuthType: "access-key", AccessKeyID: "b-key", AccessKeySecret: "b-secret"},
	}})
	for _, args := range [][]string{{"get", "zones", "-o", "json"}, {"instance", "recover", "id1", "-o", "json"}, {"delete", "instance", "id1", "--dry-run"}, {"expunge", "instance", "id1", "--dry-run"}} {
		deleting.Store(args[0] == "delete")
		before := selected.Load()
		out, stderr, err := invokeCLI(t, cfg, "", append(args, "--context", "b")...)
		if err != nil {
			t.Fatalf("%v out=%s stderr=%s", err, out, stderr)
		}
		if args[0] == "get" || args[0] == "instance" {
			if selected.Load() == before {
				t.Fatal("selected endpoint not used")
			}
		}
	}
	if wrong.Load() != 0 || readContexts(t, cfg).CurrentContext != "a" {
		t.Fatal("override changed default or accessed wrong environment")
	}
	out, stderr, err := invokeCLI(t, cfg, "", "delete", "instance", "id1", "--dry-run", "--context", "missing")
	if err == nil || out != "" || !strings.Contains(stderr, "not found") {
		t.Fatalf("invalid override: %v %s %s", err, out, stderr)
	}
}

func TestContextRefreshOnlyUpdatesSelectedCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/valid"):
			fmt.Fprint(w, `{"valid":false}`)
		case strings.HasSuffix(r.URL.Path, "/login"):
			fmt.Fprint(w, `{"inventory":{"uuid":"new-b-session"}}`)
		default:
			if r.Header.Get("Authorization") != "OAuth new-b-session" {
				t.Errorf("wrong session %s", r.Header.Get("Authorization"))
			}
			inventory(w)
		}
	}))
	defer server.Close()
	old := config.Context{Endpoint: "unused.invalid", SessionUUID: "a-session", Username: "a", Password: "a-password"}
	cfg := writeContexts(t, config.ZStackConfig{CurrentContext: "a", Contexts: map[string]config.Context{"a": old, "b": {Endpoint: strings.TrimPrefix(server.URL, "http://"), Username: "b", Password: "b-password", SessionUUID: "expired"}}})
	out, stderr, err := invokeCLI(t, cfg, "", "get", "zones", "--context", "b", "-o", "json")
	if err != nil {
		t.Fatalf("%v %s %s", err, out, stderr)
	}
	saved := readContexts(t, cfg)
	if saved.CurrentContext != "a" || saved.Contexts["a"] != old || saved.Contexts["b"].SessionUUID != "new-b-session" {
		t.Fatal("session refresh leaked across contexts")
	}
}

func TestContextManagementAndNamedLogin(t *testing.T) {
	cfg := writeContexts(t, config.ZStackConfig{CurrentContext: "a", Contexts: map[string]config.Context{"a": {Endpoint: "a", Password: "secret-password", AccessKeyID: "secret-ak", AccessKeySecret: "secret-sk", SessionUUID: "secret-session"}}})
	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := invokeCLI(t, cfg, "", args...)
		if err != nil {
			t.Fatalf("%v %s %s", err, out, stderr)
		}
		return out
	}
	run("login", "b", "--ak", "b-key", "--sk", "b-secret", "--context-name", "staging", "--no-switch")
	if readContexts(t, cfg).CurrentContext != "a" {
		t.Fatal("no-switch changed default")
	}
	out := run("config", "get-contexts", "-o", "json")
	var rows []map[string]any
	if json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 || rows[0]["name"] != "a" {
		t.Fatal(out)
	}
	for _, format := range []string{"json", "yaml", "text"} {
		out = run("config", "view", "-o", format)
		for _, secret := range []string{"secret-password", "secret-ak", "secret-sk", "secret-session", "b-secret"} {
			if strings.Contains(out, secret) {
				t.Fatalf("credential exposed in %s", format)
			}
		}
	}
	run("config", "rename-context", "staging", "test")
	run("config", "use-context", "test")
	run("config", "rename-context", "test", "prod")
	if strings.TrimSpace(run("config", "current-context")) != "prod" {
		t.Fatal("rename did not follow default")
	}
	before, _ := os.ReadFile(cfg)
	for _, args := range [][]string{{"config", "delete-context", "prod"}, {"config", "use-context", "missing"}, {"config", "rename-context", "prod", "a"}} {
		out, stderr, err := invokeCLI(t, cfg, "", args...)
		if err == nil || out != "" {
			t.Fatalf("%v %s %s", err, out, stderr)
		}
		after, _ := os.ReadFile(cfg)
		if string(after) != string(before) {
			t.Fatal("failed operation changed config")
		}
	}
	run("config", "delete-context", "a")
	saved := readContexts(t, cfg)
	if len(saved.Contexts) != 1 || saved.Contexts["prod"].AccessKeySecret != "b-secret" {
		t.Fatal("credentials lost")
	}
	info, _ := os.Stat(cfg)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %v", info.Mode())
	}
}
