package carejobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is what a test server saw of the last request.
type recorded struct {
	mu      sync.Mutex
	method  string
	path    string
	header  http.Header
	rpcID   json.RawMessage
	rpcCall struct {
		Method string `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	body []byte
}

func (r *recorded) arguments() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.rpcCall.Params.Arguments)
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes.TrimSpace(data)
}

// answerWith returns a handler that records the request and answers with a
// recorded fixture, its JSON-RPC id rewritten to the id the client sent (as a
// real server does).
func answerWith(t testing.TB, rec *recorded, fixture string) http.HandlerFunc {
	t.Helper()
	data := readFixture(t, fixture)
	return func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		rec.mu.Lock()
		rec.method, rec.path, rec.header, rec.body = r.Method, r.URL.Path, r.Header.Clone(), body.Bytes()
		_ = json.Unmarshal(rec.body, &rec.rpcCall)
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(rec.body, &req)
		rec.rpcID = req.ID
		rec.mu.Unlock()

		var msg map[string]json.RawMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Errorf("fixture %s is not a JSON object: %v", fixture, err)
		}
		msg["id"] = req.ID
		out, _ := json.Marshal(msg)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}
}

// fixtureClient returns a Client wired to a test server that answers with fixture.
func fixtureClient(t *testing.T, fixture string, opts ...Option) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(answerWith(t, rec, fixture))
	t.Cleanup(srv.Close)
	return NewClient(append([]Option{WithBaseURL(srv.URL)}, opts...)...), rec
}

func TestRequestShape(t *testing.T) {
	c, rec := fixtureClient(t, "search.json")
	if _, err := c.SearchJobs(t.Context(), SearchParams{Kind: KindNurses}); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/mcp" {
		t.Errorf("request = %s %s, want POST /api/mcp", rec.method, rec.path)
	}
	if got := rec.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.header.Get("Accept"); got != "application/json, text/event-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := rec.header.Get("User-Agent"); got != "carejobs-go/"+Version {
		t.Errorf("User-Agent = %q", got)
	}
	var req map[string]any
	if err := json.Unmarshal(rec.body, &req); err != nil {
		t.Fatal(err)
	}
	if req["jsonrpc"] != "2.0" || req["method"] != "tools/call" {
		t.Errorf("envelope = %v", req)
	}
	if rec.rpcCall.Params.Name != "search_healthcare_jobs" {
		t.Errorf("tool = %q", rec.rpcCall.Params.Name)
	}
}

func TestSearchJobs(t *testing.T) {
	c, rec := fixtureClient(t, "search.json")
	res, err := c.SearchJobs(t.Context(), SearchParams{
		Kind: KindNurses, State: "TX", Sort: SortHighestPay, Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := `{"kind":"nurses","state":"TX","sort":"highest_pay","limit":3}`
	if got := rec.arguments(); got != wantArgs {
		t.Errorf("arguments = %s, want %s", got, wantArgs)
	}
	if res.Total != 6628 || len(res.Jobs) != 3 {
		t.Fatalf("total = %d, jobs = %d", res.Total, len(res.Jobs))
	}
	if res.Pay == nil || res.Pay.Median != 45 || res.Pay.TypicalLow != 40 || res.Pay.TypicalHigh != 65 ||
		res.Pay.RateSamples != 918 || res.Pay.Priced != 1055 || res.Pay.Open != 6628 {
		t.Errorf("pay = %+v", res.Pay)
	}
	first := res.Jobs[0]
	if first.Employer != "The Helper Bees" || first.Pay != "$125/hr" || first.Kind != "Nurses" ||
		first.Location != "Uvalde, TX" || first.Listed != "2026-10-06" ||
		!reflect.DeepEqual(first.Schedule, []string{"Contract"}) ||
		first.EmployerPage != "https://carejobs.now/employers/the-helper-bees" ||
		first.Via != "" || first.Facility != "" {
		t.Errorf("first job = %+v", first)
	}
	if got := res.Jobs[1].Pay; got != "$69.14–$97.17/hr" {
		t.Errorf("escaped en dash did not decode: %q", got)
	}
	if !strings.HasPrefix(res.Text, "6,628 open healthcare jobs match") {
		t.Errorf("text = %q", res.Text)
	}
	if !strings.HasPrefix(res.More, "https://carejobs.now/?work=nurses") {
		t.Errorf("more = %q", res.More)
	}
}

func TestSearchJobsVariants(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		c, _ := fixtureClient(t, "search_empty.json")
		res, err := c.SearchJobs(t.Context(), SearchParams{Query: "zzzqqqxx"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 0 || res.Pay != nil || len(res.Jobs) != 0 || !strings.HasPrefix(res.Text, "No open healthcare jobs") {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("job boards and no employer page", func(t *testing.T) {
		c, _ := fixtureClient(t, "search_via_and_nopay.json")
		res, err := c.SearchJobs(t.Context(), SearchParams{MinHourlyUSD: 90})
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Jobs[0]; got.Via != "Adzuna" || got.EmployerPage != "" || got.Employer != "Greenstaff Medical US-Travel" {
			t.Errorf("job via a board = %+v", got)
		}
		if got := res.Jobs[1]; got.Via != "" || got.EmployerPage != "https://carejobs.now/employers/btydental" {
			t.Errorf("job with employer page = %+v", got)
		}
	})
	t.Run("unpriced", func(t *testing.T) {
		c, _ := fixtureClient(t, "search_unpriced.json")
		res, err := c.SearchJobs(t.Context(), SearchParams{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Pay != nil {
			t.Errorf("pay = %+v, want nil when no rate is measured", res.Pay)
		}
		if res.Jobs[1].Pay != "" {
			t.Errorf("a job with pay null must have empty Pay, got %q", res.Jobs[1].Pay)
		}
		if !strings.Contains(res.Jobs[0].Pay, "not a rate this posting states") {
			t.Errorf("typical-pay text lost: %q", res.Jobs[0].Pay)
		}
	})
	t.Run("placeless remote jobs", func(t *testing.T) {
		c, _ := fixtureClient(t, "search_ai.json")
		res, err := c.SearchJobs(t.Context(), SearchParams{Kind: KindAITraining})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 173 || res.Jobs[0].Kind != "AI training" || res.Jobs[1].Location != "Remote" {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestPay(t *testing.T) {
	t.Run("every kind", func(t *testing.T) {
		c, rec := fixtureClient(t, "pay_overview.json")
		res, err := c.Pay(t.Context(), PayParams{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != "{}" {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.AsOf != "2026-10-19" || res.Board == nil || res.Board.Median != 40 || res.Board.OpenJobs != 301667 ||
			res.Board.NoRate != 214164 || len(res.PayByKind) != 4 {
			t.Fatalf("result = %+v", res)
		}
		n := res.PayByKind[0]
		if n.Kind != KindNurses || n.Label != "Registered Nurses & LPNs" || n.Median != 45 || !n.MarketRate || n.Employers != 515 {
			t.Errorf("nurses = %+v", n)
		}
		if res.Pay != nil || res.BestPaying != nil {
			t.Errorf("fields of other shapes were filled: %+v", res)
		}
		if !strings.HasPrefix(res.Text, "Healthcare pay by kind of job") {
			t.Errorf("text = %q", res.Text)
		}
	})
	t.Run("a kind", func(t *testing.T) {
		c, rec := fixtureClient(t, "pay_kind.json")
		res, err := c.Pay(t.Context(), PayParams{Kind: KindNurses})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"kind":"nurses"}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.Kind != KindNurses || res.OpenJobs != 97460 || res.Pay == nil || res.Pay.Median != 45 || res.Pay.NoRate != 70317 {
			t.Fatalf("result = %+v", res)
		}
		if len(res.BestPaying) != 4 || res.BestPaying[0].Slug != "the-helper-bees" || res.BestPaying[0].Median != 125 ||
			res.BestPaying[0].MarketRate {
			t.Errorf("best paying = %+v", res.BestPaying)
		}
	})
	t.Run("an employer", func(t *testing.T) {
		c, rec := fixtureClient(t, "pay_employer.json")
		res, err := c.Pay(t.Context(), PayParams{Employer: "cvs-health"})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"employer":"cvs-health"}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.Employer != "cvs-health" || res.Pay == nil || res.Pay.Median != 30 || res.PayText != "" ||
			len(res.ByKind) != 4 || res.ByKind[0].Kind != KindPharmacy || res.ByKind[0].Label != "" {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("an employer and a kind", func(t *testing.T) {
		c, _ := fixtureClient(t, "pay_employer_kind.json")
		res, err := c.Pay(t.Context(), PayParams{Kind: KindPharmacy, Employer: "cvs-health"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Employer != "cvs-health" || res.Kind != KindPharmacy || res.OpenJobs != 5129 || res.Pay == nil || res.Pay.Median != 25 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("an employer whose pay cannot be measured", func(t *testing.T) {
		c, _ := fixtureClient(t, "pay_employer_unmeasured.json")
		res, err := c.Pay(t.Context(), PayParams{Employer: "cross-country-healthcare"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Pay != nil || res.PayText != "Posted on travel assignments, as weekly pay" || len(res.ByKind) != 0 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("an employer and a kind, unmeasured", func(t *testing.T) {
		c, _ := fixtureClient(t, "pay_employer_kind_unmeasured.json")
		res, err := c.Pay(t.Context(), PayParams{Kind: KindNurses, Employer: "cross-country-healthcare"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Pay != nil || res.OpenJobs != 4032 {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestJobMarket(t *testing.T) {
	c, rec := fixtureClient(t, "market.json")
	res, err := c.JobMarket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rec.rpcCall.Params.Name != "get_healthcare_job_market" || rec.arguments() != "{}" {
		t.Errorf("call = %s %s", rec.rpcCall.Params.Name, rec.arguments())
	}
	if res.OpenJobs != 301667 || res.SourcesLive != 1280 || res.NewLast7d != 41967 || res.NewPerWeek != nil {
		t.Errorf("counts = %+v", res)
	}
	at, err := res.UpdatedAt()
	if err != nil || at.UTC().Format("2006-01-02T15:04") != "2026-10-19T06:59" {
		t.Errorf("UpdatedAt = %v, %v", at, err)
	}
	if got := res.OpenByKind[0]; got.Slug != KindNurses || got.OpenJobs != 97460 {
		t.Errorf("open by kind = %+v", got)
	}
	if got := res.OpenByState[0]; got.Code != "TX" || got.Name != "Texas" || got.OpenJobs != 19360 {
		t.Errorf("open by state = %+v", got)
	}
	if got := res.OpenByEmployer[0]; got.Slug != "adzuna" || got.Type != EmployerTypeJobBoard || got.OpenJobs != 31995 {
		t.Errorf("open by employer = %+v", got)
	}
}

func TestJobsByState(t *testing.T) {
	t.Run("every state", func(t *testing.T) {
		c, rec := fixtureClient(t, "states_overview.json")
		res, err := c.JobsByState(t.Context(), StateParams{})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != "{}" {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if len(res.States) != 4 || res.State != "" {
			t.Fatalf("result = %+v", res)
		}
		tx := res.States[0]
		if tx.Code != "TX" || tx.OpenJobs != 11203 || tx.TopKinds[0].Kind != KindNurses || tx.TopKinds[0].OpenJobs != 3970 || tx.TopKinds[0].URL != "" {
			t.Errorf("first state = %+v", tx)
		}
	})
	t.Run("a state", func(t *testing.T) {
		c, rec := fixtureClient(t, "state_detail.json")
		res, err := c.JobsByState(t.Context(), StateParams{State: "OH", Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"state":"OH","limit":3}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.State != "OH" || res.Name != "Ohio" || res.OpenJobs != 7399 || res.Employers != 148 ||
			len(res.ByKind) != 16 || len(res.PayByKind) != 12 || len(res.Towns) != 40 || len(res.Hiring) != 12 || len(res.Newest) != 3 {
			t.Fatalf("result = %+v", res)
		}
		if got := res.ByKind[0]; got.Kind != KindNurses || got.OpenJobs != 2660 || got.URL != "https://carejobs.now/states/ohio/nurses" {
			t.Errorf("by kind = %+v", got)
		}
		if got := res.PayByKind[0]; got.Kind != KindNurses || got.P25 != 36 || got.Median != 38 || got.P75 != 48 || got.JobsWithRate != 132 || got.Employers != 19 {
			t.Errorf("pay by kind = %+v", got)
		}
		if got := res.Towns[0]; got.Key != "Columbus" || got.Jobs != 1413 || got.Slug != "columbus" {
			t.Errorf("town = %+v", got)
		}
		if got := res.Hiring[0]; got.Key != "cleveland-clinic" || got.Jobs != 1025 {
			t.Errorf("hiring = %+v", got)
		}
		if got := res.Newest[0]; got.Employer != "Legacy Healthcare Services" || got.Pay != "" || got.Where != "Tiffin, Canton and 3 more, OH" {
			t.Errorf("newest = %+v", got)
		}
	})
	t.Run("a state and a kind", func(t *testing.T) {
		c, rec := fixtureClient(t, "state_kind.json")
		res, err := c.JobsByState(t.Context(), StateParams{State: "OH", Kind: KindNursingAssistants, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"state":"OH","kind":"nursing-assistants","limit":3}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.State != "OH" || res.Kind != KindNursingAssistants || res.OpenJobs != 477 || res.Employers != 42 {
			t.Fatalf("result = %+v", res)
		}
		if res.Pay == nil || res.Pay.P25 != 19 || res.Pay.Median != 19 || res.Pay.P75 != 21 || res.Pay.JobsWithRate != 23 {
			t.Errorf("pay = %+v", res.Pay)
		}
		if len(res.Newest) != 3 || res.Towns[0].Key != "Cincinnati" || res.Hiring[0].Key != "communicare-health-services" {
			t.Errorf("slice = %+v", res)
		}
	})
	t.Run("a slice too small for a page", func(t *testing.T) {
		c, _ := fixtureClient(t, "state_kind_sparse.json")
		res, err := c.JobsByState(t.Context(), StateParams{State: "WY", Kind: KindDental})
		if err != nil {
			t.Fatal(err)
		}
		if res.OpenJobs != 2 || res.Pay != nil || len(res.Newest) != 0 || res.Employers != 0 || !strings.Contains(res.Text, "too few for a page") {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestEmployers(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		c, rec := fixtureClient(t, "employers.json")
		res, err := c.Employers(t.Context(), EmployerListParams{Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"limit":3}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		if res.Total != 2017 || len(res.Entries) != 3 {
			t.Fatalf("result = %+v", res)
		}
		a, b := res.Entries[0], res.Entries[1]
		if a.Slug != "cross-country-healthcare" || a.Type != EmployerTypeEmployer || a.HQ != "Boca Raton, FL" ||
			a.OpenJobs != 7457 || len(a.States) != 51 || a.PostedMedianHourly != 0 {
			t.Errorf("first entry = %+v", a)
		}
		if b.Slug != "cvs-health" || b.PostedMedianHourly != 30 || b.URL != "https://carejobs.now/employers/cvs-health" {
			t.Errorf("second entry = %+v", b)
		}
	})
	t.Run("all filters", func(t *testing.T) {
		c, rec := fixtureClient(t, "employers.json")
		_, err := c.Employers(t.Context(), EmployerListParams{
			Query: "care", Type: EmployerTypeAIProgram, Care: CareHomeCare, State: "OH", Kind: KindCaregivers, Limit: 5,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := `{"query":"care","type":"ai-program","care":"home-care","state":"OH","kind":"caregivers","limit":5}`
		if rec.arguments() != want {
			t.Errorf("arguments = %s, want %s", rec.arguments(), want)
		}
	})
	t.Run("none found", func(t *testing.T) {
		c, _ := fixtureClient(t, "employers_empty.json")
		res, err := c.Employers(t.Context(), EmployerListParams{Query: "zzzqq"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 0 || len(res.Entries) != 0 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("AI programs have no headquarters", func(t *testing.T) {
		c, _ := fixtureClient(t, "employers_ai.json")
		res, err := c.Employers(t.Context(), EmployerListParams{Type: EmployerTypeAIProgram})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 12 || res.Entries[0].Slug != "mindrift" || res.Entries[0].Type != EmployerTypeAIProgram || res.Entries[0].HQ != "" {
			t.Errorf("result = %+v", res.Entries[0])
		}
	})
}

func TestEmployer(t *testing.T) {
	t.Run("an employer", func(t *testing.T) {
		c, rec := fixtureClient(t, "employer.json")
		res, err := c.Employer(t.Context(), "cvs-health")
		if err != nil {
			t.Fatal(err)
		}
		if rec.rpcCall.Params.Name != "get_healthcare_employer" || rec.arguments() != `{"employer":"cvs-health"}` {
			t.Errorf("call = %s %s", rec.rpcCall.Params.Name, rec.arguments())
		}
		if res.Slug != "cvs-health" || res.Name != "CVS Health" || res.Type != EmployerTypeEmployer || res.HQ != "Woonsocket, RI" ||
			res.OpenJobs != 5655 || res.Pay == nil || res.Pay.Median != 30 || res.PayText != "" ||
			res.JobsURL != "https://carejobs.now/employers/cvs-health/jobs" || res.CareersURL != "https://jobs.cvshealth.com" {
			t.Fatalf("result = %+v", res)
		}
		if len(res.OpenByKind) != 10 || res.OpenByKind[0].Kind != KindPharmacy || res.OpenByKind[0].Pay == nil {
			t.Errorf("open by kind = %+v", res.OpenByKind)
		}
		if len(res.Newest) != 3 || len(res.BestPaid) != 3 || res.Newest[0].EmployerPage == "" {
			t.Errorf("jobs = %d newest, %d best paid", len(res.Newest), len(res.BestPaid))
		}
	})
	t.Run("a job board", func(t *testing.T) {
		c, _ := fixtureClient(t, "employer_board.json")
		res, err := c.Employer(t.Context(), "Adzuna")
		if err != nil {
			t.Fatal(err)
		}
		if res.Type != EmployerTypeJobBoard || res.HQ != "London, UK" || res.JobsURL != "" || res.CareersURL != "" ||
			res.Newest[0].Via != "Adzuna" || res.Newest[0].EmployerPage != "" {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("an AI program", func(t *testing.T) {
		c, _ := fixtureClient(t, "employer_ai.json")
		res, err := c.Employer(t.Context(), "transperfect-dataforce")
		if err != nil {
			t.Fatal(err)
		}
		if res.Type != EmployerTypeAIProgram || res.HQ != "" || res.CareersURL != "" || res.Newest[0].Location != "" {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestToolsAndCall(t *testing.T) {
	t.Run("tools", func(t *testing.T) {
		c, rec := fixtureClient(t, "tools_list.json")
		tools, err := c.Tools(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if rec.rpcCall.Method != "tools/list" {
			t.Errorf("method = %q", rec.rpcCall.Method)
		}
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
		want := []string{
			"search_healthcare_jobs", "get_healthcare_pay", "get_healthcare_job_market",
			"get_healthcare_jobs_by_state", "list_healthcare_employers", "get_healthcare_employer",
		}
		if !reflect.DeepEqual(names, want) {
			t.Errorf("tools = %v", names)
		}
		if !tools[0].Annotations.ReadOnlyHint || !tools[0].Annotations.IdempotentHint || tools[0].Annotations.DestructiveHint {
			t.Errorf("annotations = %+v", tools[0].Annotations)
		}
		if !reflect.DeepEqual(kindsOf(t, tools[0]), Kinds) {
			t.Errorf("the kind enum of search_healthcare_jobs differs from Kinds:\n%v\n%v", kindsOf(t, tools[0]), Kinds)
		}
	})
	t.Run("call", func(t *testing.T) {
		c, rec := fixtureClient(t, "search.json")
		res, err := c.Call(t.Context(), "search_healthcare_jobs", map[string]any{"limit": 3})
		if err != nil {
			t.Fatal(err)
		}
		if rec.arguments() != `{"limit":3}` {
			t.Errorf("arguments = %s", rec.arguments())
		}
		var data struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(res.Data, &data); err != nil || data.Total != 6628 || res.Text == "" {
			t.Errorf("data = %s, text = %q, err = %v", res.Data, res.Text, err)
		}
	})
}

// kindsOf reads the `kind` enum out of a tool's input schema.
func kindsOf(t *testing.T, tool Tool) []Kind {
	t.Helper()
	var schema struct {
		Properties struct {
			Kind struct {
				Enum []Kind `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	return schema.Properties.Kind.Enum
}

// TestFixturesDecodeStrictly decodes every recorded structuredContent with
// unknown fields refused, so a field the live API sends that no struct has
// fails here instead of being dropped silently.
func TestFixturesDecodeStrictly(t *testing.T) {
	cases := []struct {
		fixture string
		target  func() any
	}{
		{"search.json", func() any { return new(SearchResult) }},
		{"search_empty.json", func() any { return new(SearchResult) }},
		{"search_via_and_nopay.json", func() any { return new(SearchResult) }},
		{"search_unpriced.json", func() any { return new(SearchResult) }},
		{"search_ai.json", func() any { return new(SearchResult) }},
		{"pay_overview.json", func() any { return new(PayResult) }},
		{"pay_kind.json", func() any { return new(PayResult) }},
		{"pay_employer.json", func() any { return new(PayResult) }},
		{"pay_employer_kind.json", func() any { return new(PayResult) }},
		{"pay_employer_unmeasured.json", func() any { return new(PayResult) }},
		{"pay_employer_kind_unmeasured.json", func() any { return new(PayResult) }},
		{"market.json", func() any { return new(JobMarket) }},
		{"states_overview.json", func() any { return new(StateResult) }},
		{"state_detail.json", func() any { return new(StateResult) }},
		{"state_kind.json", func() any { return new(StateResult) }},
		{"state_kind_sparse.json", func() any { return new(StateResult) }},
		{"employers.json", func() any { return new(EmployerList) }},
		{"employers_empty.json", func() any { return new(EmployerList) }},
		{"employers_ai.json", func() any { return new(EmployerList) }},
		{"employer.json", func() any { return new(Employer) }},
		{"employer_board.json", func() any { return new(Employer) }},
		{"employer_ai.json", func() any { return new(Employer) }},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			var msg struct {
				Result struct {
					StructuredContent json.RawMessage `json:"structuredContent"`
				} `json:"result"`
			}
			if err := json.Unmarshal(readFixture(t, tc.fixture), &msg); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(bytes.NewReader(msg.Result.StructuredContent))
			dec.DisallowUnknownFields()
			if err := dec.Decode(tc.target()); err != nil {
				t.Errorf("decode: %v", err)
			}
		})
	}
}

func TestToolsDecodeStrictly(t *testing.T) {
	var msg struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(readFixture(t, "tools_list.json"), &msg); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(msg.Result.Tools))
	dec.DisallowUnknownFields()
	var tools []Tool
	if err := dec.Decode(&tools); err != nil {
		t.Errorf("decode: %v", err)
	}
}

func TestParamsEncoding(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"zero search", SearchParams{}, `{}`},
		{"full search", SearchParams{
			Query: "ICU", Kind: KindNurses, Employer: "hca", State: "TX", Remote: true,
			MinHourlyUSD: 60, Sort: SortNewest, Limit: 25,
		}, `{"query":"ICU","kind":"nurses","employer":"hca","state":"TX","remote":true,"min_hourly_usd":60,"sort":"newest","limit":25}`},
		{"zero pay", PayParams{}, `{}`},
		{"zero state", StateParams{}, `{}`},
		{"zero employers", EmployerListParams{}, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestKinds(t *testing.T) {
	want := []string{
		"nurses", "nurse-practitioners", "physicians", "nursing-assistants", "caregivers", "therapy-rehab",
		"mental-health", "pharmacy", "imaging", "lab", "surgical-techs", "emts-paramedics", "medical-assistants",
		"dental", "admin", "allied-health", "ai-training",
	}
	if len(Kinds) != 17 {
		t.Fatalf("len(Kinds) = %d, want 17", len(Kinds))
	}
	for i, k := range Kinds {
		if string(k) != want[i] {
			t.Errorf("Kinds[%d] = %q, want %q", i, k, want[i])
		}
		if !k.Valid() {
			t.Errorf("%q is not Valid", k)
		}
	}
	if Kind("plumbers").Valid() || Kind("").Valid() {
		t.Error("an unknown kind reported Valid")
	}
}

func TestToolError(t *testing.T) {
	c, _ := fixtureClient(t, "error_tool.json")
	res, err := c.Employer(t.Context(), "no-such-employer-xyz")
	if res != nil {
		t.Errorf("result = %+v, want nil", res)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if apiErr.Code != CodeTool || apiErr.Tool != "get_healthcare_employer" ||
		apiErr.Message != "Unknown employer 'no-such-employer-xyz'. list_healthcare_employers lists the employers and their slugs." {
		t.Errorf("error = %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "get_healthcare_employer") || !strings.Contains(err.Error(), "Unknown employer") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestInvalidKindIsAToolError(t *testing.T) {
	c, _ := fixtureClient(t, "error_kind.json")
	_, err := c.SearchJobs(t.Context(), SearchParams{Kind: "bogus"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != CodeTool || !strings.HasPrefix(apiErr.Message, "Unknown kind 'bogus'") {
		t.Fatalf("err = %v", err)
	}
}

func TestRPCError(t *testing.T) {
	c, _ := fixtureClient(t, "error_rpc.json")
	_, err := c.Call(t.Context(), "nope", nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != CodeRPC || apiErr.RPCCode != -32601 || apiErr.Message != "Method not found: nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantMessage string
	}{
		{"json-rpc body", 400, "application/json", `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}`, "Invalid Request"},
		{"plain body", 502, "text/plain", "Bad gateway from the edge", "Bad gateway from the edge"},
		{"long body is cut", 500, "text/html", strings.Repeat("x", 500), strings.Repeat("x", 200)},
		{"empty body", 503, "text/plain", "", "Service Unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := NewClient(WithBaseURL(srv.URL)).JobMarket(t.Context())
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Code != CodeHTTP || apiErr.Status != tc.status || apiErr.Message != tc.wantMessage {
				t.Fatalf("err = %v (%+v)", err, apiErr)
			}
		})
	}
}

func TestProtocolErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `<html>hello</html>`},
		{"no result", `{"jsonrpc":"2.0","id":1}`},
		{"result without structured content", `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hi"}]}}`},
		{"structured content is not an object", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":[1,2]}}`},
		{"structured content of the wrong type", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":{"total":"many"}}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := NewClient(WithBaseURL(srv.URL)).SearchJobs(t.Context(), SearchParams{})
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Code != CodeProtocol || apiErr.Tool != "search_healthcare_jobs" {
				t.Fatalf("err = %v (%+v)", err, apiErr)
			}
		})
	}
}

func TestEventStreamAnswer(t *testing.T) {
	data := bytes.TrimSpace(readFixture(t, "market.json"))
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		msg["id"] = req.ID
		out, _ := json.Marshal(msg)
		w.Header().Set("Content-Type", "text/event-stream")
		// A notification first (other id), then the answer, as MCP servers may stream.
		_, _ = w.Write([]byte("event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\r\n\r\n"))
		_, _ = w.Write([]byte("event: message\r\ndata: " + string(out) + "\r\n\r\n"))
	}))
	defer srv.Close()
	res, err := NewClient(WithBaseURL(srv.URL)).JobMarket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.OpenJobs != 301667 {
		t.Errorf("open jobs = %d", res.OpenJobs)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keep-alive\n\n"))
	}))
	defer empty.Close()
	_, err = NewClient(WithBaseURL(empty.URL)).JobMarket(t.Context())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != CodeProtocol {
		t.Errorf("empty stream: err = %v", err)
	}
}

// hangingServer answers nothing until the test ends. It reads the request body
// first: Go's server only notices a client that went away once the body is read.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

func TestTimeoutCancelAndNetworkErrors(t *testing.T) {
	t.Run("context deadline", func(t *testing.T) {
		srv := hangingServer(t)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err := NewClient(WithBaseURL(srv.URL)).JobMarket(ctx)
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeTimeout {
			t.Fatalf("err = %v", err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("errors.Is(err, context.DeadlineExceeded) = false for %v", err)
		}
	})
	t.Run("http client timeout", func(t *testing.T) {
		srv := hangingServer(t)
		c := NewClient(WithBaseURL(srv.URL), WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
		_, err := c.JobMarket(t.Context())
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeTimeout {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		srv := hangingServer(t)
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()
		_, err := NewClient(WithBaseURL(srv.URL)).JobMarket(ctx)
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeCanceled || !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, err := NewClient(WithBaseURL(url)).JobMarket(t.Context())
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeNetwork || apiErr.Err == nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestOptions(t *testing.T) {
	t.Run("base URL with trailing slash and user agent", func(t *testing.T) {
		rec := &recorded{}
		srv := httptest.NewServer(answerWith(t, rec, "market.json"))
		defer srv.Close()
		c := NewClient(WithBaseURL(srv.URL+"//"), WithUserAgent("my-app/2.0"))
		if _, err := c.JobMarket(t.Context()); err != nil {
			t.Fatal(err)
		}
		if rec.path != "/api/mcp" || rec.header.Get("User-Agent") != "my-app/2.0" {
			t.Errorf("path = %q, user agent = %q", rec.path, rec.header.Get("User-Agent"))
		}
	})
	t.Run("custom HTTP client is used", func(t *testing.T) {
		rec := &recorded{}
		srv := httptest.NewServer(answerWith(t, rec, "market.json"))
		defer srv.Close()
		used := false
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			used = true
			return http.DefaultTransport.RoundTrip(r)
		})}
		if _, err := NewClient(WithBaseURL(srv.URL), WithHTTPClient(hc)).JobMarket(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !used {
			t.Error("the custom HTTP client was not used")
		}
	})
	t.Run("nil HTTP client keeps the default", func(t *testing.T) {
		c := NewClient(WithHTTPClient(nil))
		if c.httpClient == nil || c.timeout != DefaultTimeout {
			t.Errorf("httpClient = %v, timeout = %v", c.httpClient, c.timeout)
		}
	})
	t.Run("defaults", func(t *testing.T) {
		c := NewClient()
		if c.baseURL != "https://carejobs.now" || c.timeout != 15*time.Second || c.userAgent != "carejobs-go/"+Version {
			t.Errorf("defaults = %q, %v, %q", c.baseURL, c.timeout, c.userAgent)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The default client gives a call 15 seconds unless the caller's context has a
// deadline of its own (a first, uncached query on the live server can take about
// 20 seconds, so callers need to be able to wait longer).
func TestDefaultTimeoutAppliesOnlyWithoutDeadline(t *testing.T) {
	deadlineOf := func(ctx context.Context, opts ...Option) (time.Duration, bool) {
		c := NewClient(opts...)
		var got time.Duration
		var has bool
		c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if d, ok := r.Context().Deadline(); ok {
				got, has = time.Until(d), true
			}
			return nil, errors.New("stop here")
		})
		_, _ = c.JobMarket(ctx)
		return got, has
	}

	if d, has := deadlineOf(t.Context()); !has || d < 14*time.Second || d > 15*time.Second {
		t.Errorf("without a context deadline: deadline = %v, %v; want about 15s", d, has)
	}

	long, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if d, has := deadlineOf(long); !has || d < 55*time.Second {
		t.Errorf("a longer context deadline must win: deadline = %v, %v", d, has)
	}

	short, cancelShort := context.WithTimeout(t.Context(), time.Second)
	defer cancelShort()
	if d, has := deadlineOf(short); !has || d > time.Second {
		t.Errorf("a shorter context deadline must win: deadline = %v, %v", d, has)
	}
}

func TestConcurrentUse(t *testing.T) {
	c, _ := fixtureClient(t, "market.json")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.JobMarket(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
