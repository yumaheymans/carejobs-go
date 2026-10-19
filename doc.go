// Package carejobs is a client for the CareJobs.Now healthcare jobs API.
//
// CareJobs.Now (https://carejobs.now) is a free US healthcare job board. Its
// jobs are read daily from the career sites of US hospitals, health systems,
// clinics and care employers, and the pay is what each posting states. The API
// is a remote MCP server at https://carejobs.now/api/mcp: free, no key. This
// package speaks to it with the standard library only.
//
// Create a Client and call its methods with a context:
//
//	c := carejobs.NewClient()
//
//	res, err := c.SearchJobs(ctx, carejobs.SearchParams{
//		Kind:  carejobs.KindNurses,
//		State: "TX",
//		Query: "ICU",
//		Limit: 5,
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(res.Total, "jobs match")
//	for _, job := range res.Jobs {
//		fmt.Println(job.Title, "|", job.Employer, "|", job.Location, "|", job.URL)
//	}
//
// The methods are SearchJobs, Pay, JobMarket, JobsByState, Employers and
// Employer, one for each of the server's six tools. Tools lists the tools
// with their schemas, and Call runs any tool and returns the undecoded answer.
//
// Every result carries Text, the plain-text summary the server writes, next to
// the typed data. Pay and JobsByState return one struct for answers of
// different shapes; the documentation of PayResult and StateResult says which
// fields each question fills.
//
// Every error is an *Error. It tells a tool error (an unknown employer, an
// invalid kind) from a transport failure:
//
//	var apiErr *carejobs.Error
//	if errors.As(err, &apiErr) && apiErr.Code == carejobs.CodeTool {
//		fmt.Println("the server said:", apiErr.Message)
//	}
//
// The jobs link to their pages on CareJobs.Now, where the user applies. The pay
// figures are the hourly rates open jobs post (before shift differentials and
// bonuses), and a market rate is only given where at least three employers post
// one. Data: https://carejobs.now/data (CC BY 4.0, credit CareJobs.Now).
package carejobs
