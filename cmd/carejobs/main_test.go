package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	carejobs "github.com/yumaheymans/carejobs-go"
)

// ---------------------------------------------------------------------------
// A fake API that replays the answers recorded in ../../testdata
// ---------------------------------------------------------------------------

type rpcCall struct {
	Method    string
	Tool      string
	Arguments map[string]any
	UserAgent string
}

type fakeAPI struct {
	*httptest.Server
	mu    sync.Mutex
	calls []rpcCall
	delay time.Duration
	// status, when not 0, is answered instead of the recorded JSON.
	status int
}

func (f *fakeAPI) recorded() []rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]rpcCall(nil), f.calls...)
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	files := map[string]string{
		"tools/list":                   "tools_list.json",
		toolSearch:                     "search.json",
		toolPay:                        "pay_overview.json",
		toolMarket:                     "market.json",
		toolState:                      "states_overview.json",
		toolEmployers:                  "employers.json",
		toolEmployer:                   "employer.json",
		toolEmployer + "/no-such-slug": "error_tool.json",
	}
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if r.URL.Path != "/api/mcp" || r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, rpcCall{body.Method, body.Params.Name, body.Params.Arguments, r.UserAgent()})
		delay, status := f.delay, f.status
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			http.Error(w, `{"error":"unavailable"}`, status)
			return
		}
		key := body.Method
		if body.Method == "tools/call" {
			key = body.Params.Name
			if slug, _ := body.Params.Arguments["employer"].(string); slug == "no-such-slug" {
				key += "/no-such-slug"
			}
		}
		file, ok := files[key]
		if !ok {
			http.Error(w, "no recording for "+key, http.StatusNotFound)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", file))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	t.Cleanup(f.Close)
	return f
}

type result struct {
	code        int
	stdout, err string
}

func runCLI(t *testing.T, argv ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), argv, &stdout, &stderr)
	return result{code, stdout.String(), stderr.String()}
}

// ---------------------------------------------------------------------------
// Parsing the command line
// ---------------------------------------------------------------------------

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name        string
		argv        []string
		positionals []string
		flags       map[string]string
	}{
		{"words and options in any order", []string{"search", "icu", "--state", "TX", "nurse", "--limit=5"},
			[]string{"search", "icu", "nurse"}, map[string]string{"state": "TX", "limit": "5"}},
		{"options before the command", []string{"--json", "--timeout", "90", "market"},
			[]string{"market"}, map[string]string{"json": "true", "timeout": "90"}},
		{"short options", []string{"-h"}, nil, map[string]string{"help": "true"}},
		{"short version", []string{"-v"}, nil, map[string]string{"version": "true"}},
		{"double dash ends the options", []string{"search", "--", "--remote", "-x"},
			[]string{"search", "--remote", "-x"}, map[string]string{}},
		{"a lone dash is a word", []string{"search", "-"}, []string{"search", "-"}, map[string]string{}},
		{"the last repeat wins", []string{"pay", "--kind", "rn", "--kind", "cna"},
			[]string{"pay"}, map[string]string{"kind": "cna"}},
		{"equals keeps a dash in the value", []string{"search", "--query=-5"},
			[]string{"search"}, map[string]string{"query": "-5"}},
		{"boolean option", []string{"search", "--remote"}, []string{"search"}, map[string]string{"remote": "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := parseArgs(tt.argv)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(inv.positionals, tt.positionals) {
				t.Errorf("positionals = %q, want %q", inv.positionals, tt.positionals)
			}
			if !reflect.DeepEqual(inv.flags, tt.flags) {
				t.Errorf("flags = %v, want %v", inv.flags, tt.flags)
			}
		})
	}
}

func TestParseArgsRejects(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"search", "--nope"}, "unknown option '--nope'"},
		{[]string{"search", "-x"}, "unknown option '-x'"},
		{[]string{"search", "-hv"}, "unknown option '-hv'"},
		{[]string{"search", "--state"}, "--state needs a value"},
		{[]string{"search", "--state", "--remote"}, "--state needs a value"},
		{[]string{"search", "--remote=yes"}, "--remote does not take a value"},
		{[]string{"--json=1", "market"}, "--json does not take a value"},
	}
	for _, tt := range tests {
		_, err := parseArgs(tt.argv)
		var ue *usageError
		if err == nil || !asUsage(err, &ue) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("parseArgs(%q) error = %v, want a usage error containing %q", tt.argv, err, tt.want)
		}
	}
}

func asUsage(err error, target **usageError) bool {
	ue, ok := err.(*usageError)
	*target = ue
	return ok
}

func build(t *testing.T, argv ...string) (*request, error) {
	t.Helper()
	inv, err := parseArgs(argv)
	if err != nil {
		t.Fatalf("parseArgs(%q): %v", argv, err)
	}
	return buildRequest(inv, inv.positionals[0])
}

func TestBuildRequest(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		tool string
		args any
	}{
		{"search with every option",
			[]string{"search", "icu", "nurse", "--kind", "rn", "--employer", "kaiser-permanente", "--state", "TX",
				"--remote", "--min-hourly", "50", "--sort", "highest_pay", "--limit", "3"},
			toolSearch,
			carejobs.SearchParams{Query: "icu nurse", Kind: carejobs.KindNurses, Employer: "kaiser-permanente", State: "TX",
				Remote: true, MinHourlyUSD: 50, Sort: carejobs.SortHighestPay, Limit: 3}},
		{"search with --query", []string{"search", "--query", "night"}, toolSearch, carejobs.SearchParams{Query: "night"}},
		{"words win over --query", []string{"search", "icu", "--query", "night"}, toolSearch, carejobs.SearchParams{Query: "icu"}},
		{"search with nothing", []string{"search"}, toolSearch, carejobs.SearchParams{}},
		{"sort is case-insensitive", []string{"search", "--sort", "NEWEST"}, toolSearch, carejobs.SearchParams{Sort: carejobs.SortNewest}},
		{"pay overview", []string{"pay"}, toolPay, carejobs.PayParams{}},
		{"pay by kind and employer", []string{"pay", "--kind", "cna", "--employer", "cvs-health"}, toolPay,
			carejobs.PayParams{Kind: carejobs.KindNursingAssistants, Employer: "cvs-health"}},
		{"market", []string{"market"}, toolMarket, struct{}{}},
		{"every state", []string{"state"}, toolState, carejobs.StateParams{}},
		{"one state", []string{"state", "TX"}, toolState, carejobs.StateParams{State: "TX"}},
		{"state with kind and limit", []string{"state", "Ohio", "--kind", "cna", "--limit", "4"}, toolState,
			carejobs.StateParams{State: "Ohio", Kind: carejobs.KindNursingAssistants, Limit: 4}},
		{"state through --state", []string{"state", "--state", "OH"}, toolState, carejobs.StateParams{State: "OH"}},
		{"the word wins over --state", []string{"state", "TX", "--state", "OH"}, toolState, carejobs.StateParams{State: "TX"}},
		{"employers with every option",
			[]string{"employers", "kaiser", "--type", "job-board", "--care", "home-care", "--state", "CA", "--kind", "np", "--limit", "9"},
			toolEmployers,
			carejobs.EmployerListParams{Query: "kaiser", Type: carejobs.EmployerTypeJobBoard, Care: carejobs.CareHomeCare,
				State: "CA", Kind: carejobs.KindNursePractitioners, Limit: 9}},
		{"employers by --query", []string{"employers", "--query", "children"}, toolEmployers, carejobs.EmployerListParams{Query: "children"}},
		{"one employer", []string{"employer", "kaiser-permanente"}, toolEmployer, map[string]string{"employer": "kaiser-permanente"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := build(t, tt.argv...)
			if err != nil {
				t.Fatal(err)
			}
			if req.tool != tt.tool || !reflect.DeepEqual(req.args, tt.args) {
				t.Errorf("request = %s %#v, want %s %#v", req.tool, req.args, tt.tool, tt.args)
			}
		})
	}
}

func TestBuildRequestGlobalOptions(t *testing.T) {
	req, err := build(t, "market", "--json", "--base-url", "http://localhost:8080/", "--timeout", "2.5")
	if err != nil {
		t.Fatal(err)
	}
	if !req.asJSON || req.baseURL != "http://localhost:8080" || req.timeout != 2500*time.Millisecond {
		t.Errorf("request = %+v", req)
	}
	req, err = build(t, "market")
	if err != nil {
		t.Fatal(err)
	}
	if req.asJSON || req.baseURL != "https://carejobs.now" || req.timeout != 60*time.Second {
		t.Errorf("defaults = %+v", req)
	}
	if req, err = build(t, "tools"); err != nil || !req.listTool {
		t.Errorf("tools request = %+v, %v", req, err)
	}
}

func TestBuildRequestRejects(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"bogus"}, "Unknown command 'bogus'"},
		{[]string{"search", "--kind", "plumbers"}, "Unknown kind 'plumbers'"},
		{[]string{"search", "--sort", "random"}, "Unknown --sort 'random'"},
		{[]string{"search", "--limit", "x"}, "--limit needs a whole number"},
		{[]string{"search", "--limit", "0"}, "--limit needs a whole number"},
		{[]string{"search", "--limit", "-1"}, "--limit needs a value"},
		{[]string{"search", "--limit", "+5"}, "--limit needs a whole number"},
		{[]string{"search", "--limit", "2.5"}, "--limit needs a whole number"},
		{[]string{"search", "--limit", "99999999999999999999"}, "--limit needs a whole number"},
		{[]string{"search", "--min-hourly", "abc"}, "--min-hourly needs a whole number"},
		{[]string{"pay", "--remote"}, "'pay' does not take --remote"},
		{[]string{"pay", "--limit", "5"}, "'pay' does not take --limit"},
		{[]string{"pay", "extra"}, "'pay' takes no arguments"},
		{[]string{"market", "extra"}, "'market' takes no arguments"},
		{[]string{"market", "--kind", "rn"}, "'market' does not take --kind"},
		{[]string{"state", "TX", "OH"}, "'state' takes one state"},
		{[]string{"state", "--query", "x"}, "'state' does not take --query"},
		{[]string{"employers", "--type", "hospital"}, "Unknown --type 'hospital'"},
		{[]string{"employers", "--care", "spa"}, "Unknown --care 'spa'"},
		{[]string{"employers", "--sort", "newest"}, "'employers' does not take --sort"},
		{[]string{"employer"}, "'employer' takes one slug"},
		{[]string{"employer", "a", "b"}, "'employer' takes one slug"},
		{[]string{"employer", "x", "--state", "TX"}, "'employer' does not take --state"},
		{[]string{"tools", "extra"}, "'tools' takes no arguments"},
		{[]string{"market", "--timeout", "0"}, "--timeout needs a number of seconds above 0"},
		{[]string{"market", "--timeout", "-3"}, "--timeout needs a value"},
		{[]string{"market", "--timeout=-3"}, "--timeout needs a number of seconds above 0"},
		{[]string{"market", "--timeout", "soon"}, "--timeout needs a number of seconds above 0"},
		{[]string{"market", "--timeout", "NaN"}, "--timeout needs a number of seconds above 0"},
		{[]string{"market", "--base-url", "carejobs.now"}, "--base-url needs an address starting with http"},
	}
	for _, tt := range tests {
		inv, err := parseArgs(tt.argv)
		if err == nil {
			_, err = buildRequest(inv, inv.positionals[0])
		}
		var ue *usageError
		if err == nil || !asUsage(err, &ue) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: error = %v, want a usage error containing %q", tt.argv, err, tt.want)
		}
	}
}

func TestParseKind(t *testing.T) {
	for _, kind := range carejobs.Kinds {
		if got, err := parseKind(string(kind)); err != nil || got != kind {
			t.Errorf("parseKind(%q) = %q, %v", kind, got, err)
		}
	}
	for alias, kind := range kindAliases {
		if !kind.Valid() {
			t.Errorf("alias %q maps to %q, which is not a kind", alias, kind)
		}
		if got, err := parseKind(alias); err != nil || got != kind {
			t.Errorf("parseKind(%q) = %q, %v, want %q", alias, got, err, kind)
		}
	}
	for input, want := range map[string]carejobs.Kind{
		"rn":                   carejobs.KindNurses,
		"RN":                   carejobs.KindNurses,
		"Registered Nurse":     carejobs.KindNurses,
		"registered_nurse":     carejobs.KindNurses,
		"  CNA ":               carejobs.KindNursingAssistants,
		"np":                   carejobs.KindNursePractitioners,
		"PA":                   carejobs.KindNursePractitioners,
		"doctor":               carejobs.KindPhysicians,
		"Nursing Assistants":   carejobs.KindNursingAssistants,
		"Emts_Paramedics":      carejobs.KindEMTsParamedics,
		"home health aide":     carejobs.KindCaregivers,
		"therapist":            carejobs.KindTherapyRehab,
		"pharmacist":           carejobs.KindPharmacy,
		"ma":                   carejobs.KindMedicalAssistants,
		"physician  assistant": carejobs.KindNursePractitioners,
	} {
		got, err := parseKind(input)
		if err != nil || got != want {
			t.Errorf("parseKind(%q) = %q, %v, want %q", input, got, err, want)
		}
	}
	if _, err := parseKind(""); err == nil {
		t.Error("an empty kind should be refused")
	}
}

func TestChoicesMatchPackage(t *testing.T) {
	if len(cares) != 17 {
		t.Errorf("cares has %d entries, want the 17 the package knows", len(cares))
	}
	seen := map[carejobs.Care]bool{}
	for _, c := range cares {
		if c == "" || seen[c] {
			t.Errorf("care %q is empty or listed twice", c)
		}
		seen[c] = true
	}
	if got := join(sorts); got != "mixed, newest, highest_pay" {
		t.Errorf("sorts = %s", got)
	}
	if got := join(employerTypes); got != "employer, ai-program, job-board" {
		t.Errorf("employer types = %s", got)
	}
}

// ---------------------------------------------------------------------------
// Running commands against the fake API
// ---------------------------------------------------------------------------

func TestVersionAndHelp(t *testing.T) {
	if version != "1.1.0" {
		t.Errorf("version = %q, want 1.1.0", version)
	}
	for _, argv := range [][]string{{"--version"}, {"-v"}, {"search", "--version"}} {
		if res := runCLI(t, argv...); res.code != 0 || res.stdout != "1.1.0\n" || res.err != "" {
			t.Errorf("%q: %+v", argv, res)
		}
	}
	for _, argv := range [][]string{{"--help"}, {"-h"}, {"help"}, {"search", "-h"}} {
		res := runCLI(t, argv...)
		if res.code != 0 || !strings.Contains(res.stdout, "carejobs search [words]") || !strings.Contains(res.stdout, "carejobs tools") ||
			!strings.Contains(res.stdout, "nurse-practitioners") {
			t.Errorf("%q: code %d, stdout %q", argv, res.code, res.stdout)
		}
	}
	res := runCLI(t)
	if res.code != 2 || res.stdout != "" || !strings.Contains(res.err, "Usage:") {
		t.Errorf("no arguments: %+v", res)
	}
}

func TestUsageErrorsExit2WithoutCallingTheAPI(t *testing.T) {
	api := newFakeAPI(t)
	for _, argv := range [][]string{
		{"bogus"},
		{"search", "--kind", "plumbers"},
		{"search", "--sort", "random"},
		{"search", "--limit", "x"},
		{"pay", "--remote"},
		{"market", "extra"},
		{"employer"},
		{"search", "--nope"},
		{"state", "TX", "OH"},
		{"market", "--timeout", "0"},
	} {
		res := runCLI(t, append(argv, "--base-url", api.URL)...)
		if res.code != 2 || res.stdout != "" || !strings.HasPrefix(res.err, "carejobs: ") || !strings.Contains(res.err, "carejobs --help") {
			t.Errorf("%q: %+v", argv, res)
		}
	}
	if calls := api.recorded(); len(calls) != 0 {
		t.Errorf("a usage error reached the API: %+v", calls)
	}
}

func TestSearchSendsTheArgumentsAndPrintsTheSummary(t *testing.T) {
	api := newFakeAPI(t)
	res := runCLI(t, "search", "icu nurse", "--kind", "rn", "--state", "TX", "--remote", "--min-hourly", "50",
		"--sort", "newest", "--limit", "3", "--base-url", api.URL)
	if res.code != 0 || res.err != "" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.stdout, "open healthcare jobs match") || !strings.HasSuffix(res.stdout, "\n") || strings.HasSuffix(res.stdout, "\n\n") {
		t.Errorf("stdout = %q", res.stdout)
	}
	calls := api.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	want := map[string]any{
		"query": "icu nurse", "kind": "nurses", "state": "TX", "remote": true,
		"min_hourly_usd": float64(50), "sort": "newest", "limit": float64(3),
	}
	if calls[0].Method != "tools/call" || calls[0].Tool != toolSearch || !reflect.DeepEqual(calls[0].Arguments, want) {
		t.Errorf("call = %+v, want arguments %v", calls[0], want)
	}
	if calls[0].UserAgent != "carejobs-cli/1.1.0" {
		t.Errorf("User-Agent = %q", calls[0].UserAgent)
	}
}

func TestEveryCommandRuns(t *testing.T) {
	api := newFakeAPI(t)
	tests := []struct {
		argv []string
		tool string
		text string
	}{
		{[]string{"search"}, toolSearch, "open healthcare jobs"},
		{[]string{"pay"}, toolPay, ""},
		{[]string{"market"}, toolMarket, ""},
		{[]string{"state"}, toolState, ""},
		{[]string{"employers"}, toolEmployers, ""},
		{[]string{"employer", "cvs-health"}, toolEmployer, ""},
	}
	for _, tt := range tests {
		res := runCLI(t, append(tt.argv, "--base-url", api.URL)...)
		if res.code != 0 || res.err != "" || strings.TrimSpace(res.stdout) == "" || !strings.Contains(res.stdout, tt.text) {
			t.Errorf("%q: %+v", tt.argv, res)
		}
		raw := runCLI(t, append(tt.argv, "--json", "--base-url", api.URL)...)
		if raw.code != 0 || !json.Valid([]byte(raw.stdout)) || !strings.HasPrefix(raw.stdout, "{\n  \"") {
			t.Errorf("%q --json: %+v", tt.argv, raw)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw.stdout), &data); err != nil || len(data) == 0 {
			t.Errorf("%q --json: %v, %v", tt.argv, data, err)
		}
		if _, hasText := data["text"]; hasText {
			t.Errorf("%q --json carries the text summary", tt.argv)
		}
	}
	calls := api.recorded()
	if len(calls) != 2*len(tests) {
		t.Fatalf("got %d calls, want %d", len(calls), 2*len(tests))
	}
	for i, tt := range tests {
		if calls[2*i].Tool != tt.tool {
			t.Errorf("%q called %s, want %s", tt.argv, calls[2*i].Tool, tt.tool)
		}
	}
}

func TestJSONKeepsTheAPIsData(t *testing.T) {
	api := newFakeAPI(t)
	res := runCLI(t, "search", "--json", "--base-url", api.URL)
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	var got struct {
		Total int `json:"total"`
		Jobs  []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 6628 || len(got.Jobs) != 3 || !strings.HasPrefix(got.Jobs[0].URL, "https://carejobs.now/jobs/") {
		t.Errorf("data = %+v", got)
	}
}

func TestToolsCommand(t *testing.T) {
	api := newFakeAPI(t)
	res := runCLI(t, "tools", "--base-url", api.URL)
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], toolSearch+" ") || !strings.Contains(res.stdout, "Healthcare employer") {
		t.Errorf("stdout = %q", res.stdout)
	}
	raw := runCLI(t, "tools", "--json", "--base-url", api.URL)
	var tools []struct {
		Name        string          `json:"name"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal([]byte(raw.stdout), &tools); err != nil || len(tools) != 6 || !json.Valid(tools[0].InputSchema) || len(tools[0].InputSchema) < 10 {
		t.Errorf("--json: %v, %q", err, raw.stdout)
	}
	if calls := api.recorded(); len(calls) != 2 || calls[0].Method != "tools/list" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestAPIErrorsExit1(t *testing.T) {
	api := newFakeAPI(t)
	res := runCLI(t, "employer", "no-such-slug", "--base-url", api.URL)
	if res.code != 1 || res.stdout != "" || !strings.HasPrefix(res.err, "carejobs: Unknown employer") || strings.Contains(res.err, "carejobs get_healthcare_employer") {
		t.Errorf("tool error: %+v", res)
	}

	api.mu.Lock()
	api.status = http.StatusServiceUnavailable
	api.mu.Unlock()
	res = runCLI(t, "market", "--base-url", api.URL)
	if res.code != 1 || res.stdout != "" || !strings.Contains(res.err, "HTTP 503") {
		t.Errorf("http error: %+v", res)
	}

	res = runCLI(t, "market", "--base-url", "http://127.0.0.1:1", "--timeout", "5")
	if res.code != 1 || !strings.HasPrefix(res.err, "carejobs: could not reach http://127.0.0.1:1/api/mcp") {
		t.Errorf("network error: %+v", res)
	}
}

func TestTimeout(t *testing.T) {
	api := newFakeAPI(t)
	api.delay = 2 * time.Second
	start := time.Now()
	res := runCLI(t, "market", "--base-url", api.URL, "--timeout", "0.1")
	if res.code != 1 || !strings.Contains(res.err, "no answer from") || !strings.Contains(res.err, "--timeout") {
		t.Errorf("%+v", res)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v, the 0.1 s timeout was not applied", elapsed)
	}
}

func TestCancel(t *testing.T) {
	api := newFakeAPI(t)
	api.delay = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"market", "--base-url", api.URL}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "canceled") {
		t.Errorf("code %d, stderr %q", code, stderr.String())
	}
}
