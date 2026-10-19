# carejobs-go

Go client for the [CareJobs.Now](https://carejobs.now) healthcare jobs API. Search open US healthcare jobs, see the pay employers post, and look up employers and states.

- Free, no API key.
- Standard library only. Go 1.24 or newer.
- Context-aware methods, typed results, one `*carejobs.Error` type.
- The same API is a remote MCP server at <https://carejobs.now/api/mcp>, so any MCP client can use it too.

## Where the data comes from

CareJobs.Now is a free US healthcare job board. Its jobs are read daily from the career sites of US hospitals, health systems, clinics and care employers, and the pay is what each posting states (hourly rates before shift differentials and bonuses). Every job links to its page on CareJobs.Now, where the user applies. Data and docs: <https://carejobs.now/data> (CC BY 4.0, credit CareJobs.Now).

## Install

```sh
go get github.com/yumaheymans/carejobs-go
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	carejobs "github.com/yumaheymans/carejobs-go"
)

func main() {
	c := carejobs.NewClient()

	res, err := c.SearchJobs(context.Background(), carejobs.SearchParams{
		Kind:  carejobs.KindNurses,
		State: "TX",
		Query: "ICU",
		Limit: 5,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.Total, "jobs match")
	for _, job := range res.Jobs {
		fmt.Println(job.Title, "|", job.Employer, "|", job.Location, "|", job.URL)
	}

	// Every result also carries the server's readable summary:
	fmt.Println(res.Text)
}
```

A runnable version is in [`examples/search`](examples/search/main.go): `go run ./examples/search OH`.

## Client

```go
c := carejobs.NewClient(
	carejobs.WithBaseURL("https://carejobs.now"),     // default
	carejobs.WithHTTPClient(&http.Client{}),          // default: a client with no timeout of its own
	carejobs.WithUserAgent("my-app/1.0"),             // default: "carejobs-go/<version>"
)
```

A `Client` is safe for concurrent use.

| Method | What it answers | Result |
| --- | --- | --- |
| `SearchJobs(ctx, SearchParams)` | Open jobs by query, kind, employer, state, remote, minimum hourly pay, sort, limit | `*SearchResult` |
| `Pay(ctx, PayParams)` | Posted hourly pay: every kind, one kind, one employer, or both | `*PayResult` |
| `JobMarket(ctx)` | How many jobs are open, by kind, state and employer | `*JobMarket` |
| `JobsByState(ctx, StateParams)` | Every state, one state, or one kind of job in one state | `*StateResult` |
| `Employers(ctx, EmployerListParams)` | The employers, AI-training programs and job boards the site reads | `*EmployerList` |
| `Employer(ctx, slug)` | One employer by slug or name | `*Employer` |
| `Tools(ctx)` | The server's tools with their input schemas | `[]Tool` |
| `Call(ctx, name, args)` | Any tool, undecoded | `*RawResult` |

Every parameter is optional (the zero value leaves a filter off), and every result has a `Text` field with the plain-text summary the server writes.

`Pay` and `JobsByState` return one struct for answers of different shapes. The documentation of `PayResult` and `StateResult` lists which fields each question fills. Where the API sends `null` for "none", the Go field holds its zero value (an empty string, `0`, or a nil pointer or slice): `PayResult.Pay` is nil when too few of an employer's jobs post an hourly rate to measure, and `PayText` then says how the employer posts pay.

```go
pay, err := c.Pay(ctx, carejobs.PayParams{Kind: carejobs.KindNurses})
if err != nil {
	log.Fatal(err)
}
fmt.Printf("median $%.0f/hr, typical $%.0f to $%.0f/hr\n",
	pay.Pay.Median, pay.Pay.TypicalLow, pay.Pay.TypicalHigh)
for _, e := range pay.BestPaying {
	fmt.Println(e.Name, e.Median)
}

ohio, err := c.JobsByState(ctx, carejobs.StateParams{State: "OH", Kind: carejobs.KindNursingAssistants})
```

### Kinds

`carejobs.Kinds` lists the 17 kinds of job, each with a constant: `KindNurses`, `KindNursePractitioners`, `KindPhysicians`, `KindNursingAssistants`, `KindCaregivers`, `KindTherapyRehab`, `KindMentalHealth`, `KindPharmacy`, `KindImaging`, `KindLab`, `KindSurgicalTechs`, `KindEMTsParamedics`, `KindMedicalAssistants`, `KindDental`, `KindAdmin`, `KindAlliedHealth`, `KindAITraining`. `Sort`, `EmployerType` and `Care` have constants too.

### Errors

Every error is an `*carejobs.Error`:

```go
var apiErr *carejobs.Error
if errors.As(err, &apiErr) {
	switch apiErr.Code {
	case carejobs.CodeTool: // the server ran the tool and said no: apiErr.Message has its text
	case carejobs.CodeRPC: // a JSON-RPC error (apiErr.RPCCode)
	case carejobs.CodeHTTP: // a non-2xx status (apiErr.Status)
	case carejobs.CodeTimeout, carejobs.CodeCanceled, carejobs.CodeNetwork:
	case carejobs.CodeProtocol: // the answer was not what MCP promises
	}
}
```

`errors.Is(err, context.DeadlineExceeded)` and `errors.Is(err, context.Canceled)` work.

### Timeouts

Without `WithHTTPClient`, a call gives up after 15 seconds unless its context has a deadline of its own, in which case the context decides. The server answers a query it has not seen before in up to about 20 seconds, and repeats of it in a fraction of a second, so give a first call a longer deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
defer cancel()
res, err := c.SearchJobs(ctx, params)
```

With `WithHTTPClient`, your client and the context decide.

## Tests

```sh
go test ./...                        # unit tests, offline, on recorded answers in testdata/
go test -tags live -run Live -v ./... # the same questions against the real API
```

The unit tests replay real answers recorded from the live API (long lists are trimmed). A strict-decode test refuses any field that a recorded answer has and the structs do not, and the live test does the same against the live answers and also checks the `Kind` constants against the server's own list.

## License

MIT. See [LICENSE](LICENSE).
