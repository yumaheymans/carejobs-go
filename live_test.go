//go:build live

package carejobs

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// Run against the real API with:
//
//	go test -tags live -run Live -v ./...
//
// An uncached query can take about 20 seconds on the server, so each call gets
// a minute.

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// decodesStrictly runs a tool live and decodes its answer with unknown fields
// refused, so a field the server added shows up here.
func decodesStrictly(t *testing.T, c *Client, tool string, args any, into any) {
	t.Helper()
	raw, err := c.Call(liveCtx(t), tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Errorf("%s: %v", tool, err)
	}
}

func TestLive(t *testing.T) {
	c := NewClient()

	t.Run("SearchJobs", func(t *testing.T) {
		res, err := c.SearchJobs(liveCtx(t), SearchParams{Kind: KindNurses, State: "TX", Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total == 0 || len(res.Jobs) == 0 || len(res.Jobs) > 3 || res.Text == "" || res.Jobs[0].URL == "" {
			t.Errorf("result = %+v", res)
		}
		decodesStrictly(t, c, "search_healthcare_jobs", SearchParams{Kind: KindNurses, State: "TX", Limit: 3}, new(SearchResult))
	})

	t.Run("Pay", func(t *testing.T) {
		all, err := c.Pay(liveCtx(t), PayParams{})
		if err != nil {
			t.Fatal(err)
		}
		if all.Board == nil || len(all.PayByKind) != len(Kinds) {
			t.Errorf("overview = %+v", all)
		}
		byKind, err := c.Pay(liveCtx(t), PayParams{Kind: KindNurses})
		if err != nil {
			t.Fatal(err)
		}
		if byKind.Kind != KindNurses || byKind.Pay == nil || len(byKind.BestPaying) == 0 {
			t.Errorf("by kind = %+v", byKind)
		}
		slug := firstEmployer(t, c)
		byEmployer, err := c.Pay(liveCtx(t), PayParams{Employer: slug})
		if err != nil {
			t.Fatal(err)
		}
		if byEmployer.Employer != slug {
			t.Errorf("by employer = %+v", byEmployer)
		}
		decodesStrictly(t, c, "get_healthcare_pay", PayParams{}, new(PayResult))
		decodesStrictly(t, c, "get_healthcare_pay", PayParams{Kind: KindNurses}, new(PayResult))
		decodesStrictly(t, c, "get_healthcare_pay", PayParams{Employer: slug}, new(PayResult))
		decodesStrictly(t, c, "get_healthcare_pay", PayParams{Employer: slug, Kind: KindNurses}, new(PayResult))
	})

	t.Run("JobMarket", func(t *testing.T) {
		res, err := c.JobMarket(liveCtx(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.UpdatedAt(); err != nil || res.OpenJobs == 0 || len(res.OpenByKind) != len(Kinds) || len(res.OpenByState) < 50 {
			t.Errorf("market: UpdatedAt err = %v, %+v", err, res.OpenJobs)
		}
		decodesStrictly(t, c, "get_healthcare_job_market", nil, new(JobMarket))
	})

	t.Run("JobsByState", func(t *testing.T) {
		all, err := c.JobsByState(liveCtx(t), StateParams{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all.States) < 50 {
			t.Errorf("states = %d", len(all.States))
		}
		one, err := c.JobsByState(liveCtx(t), StateParams{State: "OH", Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if one.State != "OH" || one.Name != "Ohio" || len(one.Newest) == 0 {
			t.Errorf("state = %+v", one)
		}
		kind, err := c.JobsByState(liveCtx(t), StateParams{State: "OH", Kind: KindNursingAssistants, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if kind.Kind != KindNursingAssistants || kind.OpenJobs == 0 {
			t.Errorf("state and kind = %+v", kind)
		}
		decodesStrictly(t, c, "get_healthcare_jobs_by_state", StateParams{}, new(StateResult))
		decodesStrictly(t, c, "get_healthcare_jobs_by_state", StateParams{State: "OH", Limit: 3}, new(StateResult))
		decodesStrictly(t, c, "get_healthcare_jobs_by_state", StateParams{State: "OH", Kind: KindNursingAssistants, Limit: 3}, new(StateResult))
		decodesStrictly(t, c, "get_healthcare_jobs_by_state", StateParams{State: "WY", Kind: KindDental}, new(StateResult))
	})

	t.Run("Employers and Employer", func(t *testing.T) {
		list, err := c.Employers(liveCtx(t), EmployerListParams{Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if list.Total == 0 || len(list.Entries) != 3 {
			t.Fatalf("list = %+v", list)
		}
		e, err := c.Employer(liveCtx(t), list.Entries[0].Slug)
		if err != nil {
			t.Fatal(err)
		}
		if e.Slug != list.Entries[0].Slug || e.OpenJobs == 0 || len(e.Newest) == 0 || e.URL == "" {
			t.Errorf("employer = %+v", e)
		}
		decodesStrictly(t, c, "list_healthcare_employers", EmployerListParams{Limit: 3}, new(EmployerList))
		decodesStrictly(t, c, "list_healthcare_employers", EmployerListParams{Type: EmployerTypeAIProgram, Limit: 3}, new(EmployerList))
		decodesStrictly(t, c, "get_healthcare_employer", map[string]string{"employer": list.Entries[0].Slug}, new(Employer))
		decodesStrictly(t, c, "get_healthcare_employer", map[string]string{"employer": "Adzuna"}, new(Employer))
	})

	t.Run("tool errors", func(t *testing.T) {
		_, err := c.Employer(liveCtx(t), "no-such-employer-xyz")
		var apiErr *Error
		if !asError(err, &apiErr) || apiErr.Code != CodeTool {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("the Kind constants match the server", func(t *testing.T) {
		tools, err := c.Tools(liveCtx(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range tools {
			if !tool.Annotations.ReadOnlyHint {
				t.Errorf("%s: annotations = %+v", tool.Name, tool.Annotations)
			}
		}
		if len(tools) != 6 {
			t.Errorf("the server has %d tools, this module knows 6", len(tools))
		}
		for _, tool := range tools {
			var schema struct {
				Properties map[string]struct {
					Enum []Kind `json:"enum"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			if prop, ok := schema.Properties["kind"]; ok && !reflect.DeepEqual(prop.Enum, Kinds) {
				t.Errorf("%s: kind enum on the server is %v, Kinds is %v", tool.Name, prop.Enum, Kinds)
			}
		}
	})
}

// firstEmployer returns the slug of the employer with the most open jobs.
func firstEmployer(t *testing.T, c *Client) string {
	t.Helper()
	list, err := c.Employers(liveCtx(t), EmployerListParams{Limit: 1})
	if err != nil || len(list.Entries) == 0 {
		t.Fatalf("employers: %v", err)
	}
	return list.Entries[0].Slug
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
