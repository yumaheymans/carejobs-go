package carejobs

import (
	"encoding/json"
	"time"
)

// Every result carries Text: the plain-text summary the server writes for the
// answer, handy to show a person or to hand to a language model. The other
// fields are the answer's data, named after the keys the API sends.
//
// Where the API sends null for "none", the Go field holds its zero value (an
// empty string, 0, or a nil pointer or slice).

// ---------------------------------------------------------------------------
// Parameters
// ---------------------------------------------------------------------------

// SearchParams filters SearchJobs. Every field is optional; the zero value of a
// field leaves that filter off.
type SearchParams struct {
	// Query is words every job must carry in its title, place, department or
	// schedule, for example "ICU", "night", "travel" or "Houston".
	Query string `json:"query,omitempty"`
	// Kind limits the search to one kind of job.
	Kind Kind `json:"kind,omitempty"`
	// Employer is an employer, AI-training program or job board: its slug
	// (for example "kaiser-permanente") or its name. Employers finds the slugs.
	Employer string `json:"employer,omitempty"`
	// State is a US state: its two-letter code ("TX") or its name ("Texas").
	State string `json:"state,omitempty"`
	// Remote limits the search to remote (work-from-home) jobs.
	Remote bool `json:"remote,omitempty"`
	// MinHourlyUSD keeps only jobs whose posted hourly ceiling reaches this many
	// US dollars (1 to 1000).
	MinHourlyUSD int `json:"min_hourly_usd,omitempty"`
	// Sort is the order of the results; the server's default is SortMixed.
	Sort Sort `json:"sort,omitempty"`
	// Limit is how many jobs to return (1 to 25, server default 10).
	Limit int `json:"limit,omitempty"`
}

// PayParams selects what Pay measures. With both fields empty it covers every
// kind of job; Kind alone gives that job's pay and the employers posting the
// most for it; Employer alone gives that employer's pay overall and by kind;
// both give that employer's pay for that job.
type PayParams struct {
	// Kind is one kind of job.
	Kind Kind `json:"kind,omitempty"`
	// Employer is an employer slug or name.
	Employer string `json:"employer,omitempty"`
}

// StateParams selects what JobsByState returns. With no State it covers every
// US state; with a State, that state; with a State and a Kind, that kind of job
// in that state. A Kind without a State is an error the server reports.
type StateParams struct {
	// State is a US state: two-letter code or name.
	State string `json:"state,omitempty"`
	// Kind is one kind of job; it needs a State.
	Kind Kind `json:"kind,omitempty"`
	// Limit is how many of the newest jobs to return for a state (1 to 25,
	// server default 10).
	Limit int `json:"limit,omitempty"`
}

// EmployerListParams filters Employers. Every field is optional.
type EmployerListParams struct {
	// Query is part of the name, for example "Kaiser", "children" or "dialysis".
	Query string `json:"query,omitempty"`
	// Type says whether to list employers (the default), AI-training programs or
	// job boards.
	Type EmployerType `json:"type,omitempty"`
	// Care is a kind of care, for example CareHomeCare.
	Care Care `json:"care,omitempty"`
	// State keeps employers with open jobs in this US state.
	State string `json:"state,omitempty"`
	// Kind keeps employers with open jobs of this kind.
	Kind Kind `json:"kind,omitempty"`
	// Limit is how many to return (1 to 100, server default 20).
	Limit int `json:"limit,omitempty"`
}

// ---------------------------------------------------------------------------
// Shared pieces
// ---------------------------------------------------------------------------

// PayStats is posted hourly pay in US dollars, measured from the hourly rates
// open jobs state (before shift differentials and bonuses).
type PayStats struct {
	// Median is the median posted hourly rate.
	Median float64 `json:"median"`
	// TypicalLow is the 25th percentile of what jobs pay (the middle of each
	// advertised range).
	TypicalLow float64 `json:"typical_low"`
	// TypicalHigh is the 90th-percentile advertised ceiling.
	TypicalHigh float64 `json:"typical_high"`
	// RateSamples is how many distinct posted hourly rates went in.
	RateSamples int `json:"rate_samples"`
	// Employers is how many employers posted them.
	Employers int `json:"employers"`
	// OpenJobs is how many open jobs the measure covers.
	OpenJobs int `json:"open_jobs"`
	// NoRate is how many of those jobs post no hourly rate.
	NoRate int `json:"no_rate"`
	// MarketRate is true only where at least three employers post a rate.
	// It is false on the board-wide Board figures, which do not carry it.
	MarketRate bool `json:"market_rate"`
}

// Job is one open job as SearchJobs and Employer return it.
type Job struct {
	Title    string `json:"title"`
	Employer string `json:"employer"`
	// Via names the job board the job was read through, when it is another
	// employer's job. Empty for a job read from the employer's own career site.
	Via string `json:"via"`
	// Kind is the label of the kind of job, for example "Nurses" or "Doctors".
	Kind string `json:"kind"`
	// Pay is the posted pay as text (for example "$42.43-$86.19/hr" or
	// "$80,000.00 - $95,000.00/year"). Empty when the posting states none. For
	// a posting with no rate the server may give the typical pay of its employer
	// and kind instead, and then says in the text that it is not a rate the
	// posting states.
	Pay string `json:"pay"`
	// Schedule lists the job's schedule tags, for example "Full-time".
	Schedule []string `json:"schedule"`
	// Facility is the facility named in the posting, when it names one.
	Facility string `json:"facility"`
	// Location is the place of the job. Empty for remote or placeless jobs.
	Location string `json:"location"`
	// Listed is the ISO date (YYYY-MM-DD) the job was first listed.
	Listed string `json:"listed"`
	// URL is the job's page on CareJobs.Now, where the user applies.
	URL string `json:"url"`
	// EmployerPage is the employer's page on CareJobs.Now, when it has one.
	EmployerPage string `json:"employer_page"`
}

// ---------------------------------------------------------------------------
// SearchJobs
// ---------------------------------------------------------------------------

// SearchResult is the answer of SearchJobs.
type SearchResult struct {
	// Total is how many open jobs match the filters (all of them, not only the
	// ones returned in Jobs).
	Total int `json:"total"`
	// Pay is the posted pay across all matching jobs. Nil when none post a rate.
	Pay *SearchPay `json:"pay"`
	// Jobs are the jobs returned, at most the Limit.
	Jobs []Job `json:"jobs"`
	// More is the same search on the CareJobs.Now site.
	More string `json:"more"`
	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// SearchPay is the posted pay across the jobs a search matches.
type SearchPay struct {
	TypicalLow  float64 `json:"typical_low"`
	TypicalHigh float64 `json:"typical_high"`
	Median      float64 `json:"median"`
	// RateSamples is how many distinct posted hourly rates went in.
	RateSamples int `json:"rate_samples"`
	// Priced is how many matching jobs post an hourly rate.
	Priced int `json:"priced"`
	// Open is how many jobs match.
	Open int `json:"open"`
}

// ---------------------------------------------------------------------------
// Pay
// ---------------------------------------------------------------------------

// KindPay is the pay of one kind of job. Label is set in the all-kinds answer
// and empty in an employer's by-kind list.
type KindPay struct {
	Kind  Kind   `json:"kind"`
	Label string `json:"label"`
	PayStats
}

// EmployerPay is the pay one employer posts, as listed in BestPaying.
type EmployerPay struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	PayStats
}

// PayResult is the answer of Pay. Its shape follows the PayParams, and only the
// fields of that shape are filled:
//
//   - no parameters: AsOf, TypicalPayDefinition, Board, PayByKind, NoMarketRate
//   - Kind: AsOf, Kind, OpenJobs, Pay, BestPaying
//   - Employer: AsOf, Employer, Pay, PayText, ByKind
//   - Kind and Employer: AsOf, Employer, Kind, OpenJobs, Pay
//
// Pay is nil when too few of the employer's open jobs post an hourly rate to
// measure; PayText then says how that employer posts pay (for example weekly).
type PayResult struct {
	// AsOf is the ISO date the figures were measured.
	AsOf string `json:"as_of"`

	// TypicalPayDefinition explains how TypicalLow and TypicalHigh are measured.
	TypicalPayDefinition string `json:"typical_pay_definition"`
	// Board is the pay across every open job (MarketRate is not set on it).
	Board *PayStats `json:"board"`
	// PayByKind is the pay of every kind of job.
	PayByKind []KindPay `json:"pay_by_kind"`
	// NoMarketRate lists the kinds without a market rate. The API sends it as an
	// array whose entries have not appeared yet, so they are kept undecoded.
	NoMarketRate []json.RawMessage `json:"no_market_rate"`

	// Kind is the kind of job the answer is about.
	Kind Kind `json:"kind"`
	// Employer is the employer's slug.
	Employer string `json:"employer"`
	// OpenJobs is how many open jobs of that kind (or employer and kind) there are.
	OpenJobs int `json:"open_jobs"`
	// Pay is the measured pay, or nil when it cannot be measured.
	Pay *PayStats `json:"pay"`
	// PayText describes how an employer posts pay when Pay is nil.
	PayText string `json:"pay_text"`
	// BestPaying lists the employers posting the most for the kind of job.
	BestPaying []EmployerPay `json:"best_paying"`
	// ByKind is an employer's pay by kind of job.
	ByKind []KindPay `json:"by_kind"`

	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// JobMarket
// ---------------------------------------------------------------------------

// JobMarket is the answer of Client.JobMarket: how many jobs are open and how
// they split by kind, state and employer.
type JobMarket struct {
	// OpenJobs is how many healthcare jobs are open right now.
	OpenJobs int `json:"open_jobs"`
	// SourcesLive is how many employers and job boards have one or more open jobs.
	SourcesLive int `json:"sources_live"`
	// NewLast7d is how many jobs were first listed in the last 7 days.
	NewLast7d int `json:"new_last_7d"`
	// NewPerWeek is how many jobs are listed per week. Nil while the server has
	// no figure.
	NewPerWeek *float64 `json:"new_per_week"`
	// Updated is the RFC 3339 timestamp (UTC) of the latest read of the career
	// sites. UpdatedAt parses it.
	Updated string `json:"updated"`
	// OpenByKind is the open jobs of every kind of job.
	OpenByKind []JobMarketKind `json:"open_by_kind"`
	// OpenByState is the open jobs of every US state.
	OpenByState []JobMarketState `json:"open_by_state"`
	// OpenByEmployer is the open jobs of every employer, most first.
	OpenByEmployer []JobMarketEmployer `json:"open_by_employer"`
	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// UpdatedAt parses Updated.
func (m *JobMarket) UpdatedAt() (time.Time, error) {
	return time.Parse(time.RFC3339, m.Updated)
}

// JobMarketKind is the open jobs of one kind of job.
type JobMarketKind struct {
	Slug     Kind   `json:"slug"`
	Label    string `json:"label"`
	OpenJobs int    `json:"open_jobs"`
}

// JobMarketState is the open jobs of one US state.
type JobMarketState struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	OpenJobs int    `json:"open_jobs"`
	URL      string `json:"url"`
}

// JobMarketEmployer is the open jobs of one employer, AI-training program or
// job board.
type JobMarketEmployer struct {
	Slug     string       `json:"slug"`
	Name     string       `json:"name"`
	Type     EmployerType `json:"type"`
	OpenJobs int          `json:"open_jobs"`
}

// ---------------------------------------------------------------------------
// JobsByState
// ---------------------------------------------------------------------------

// KindCount is the open jobs of one kind of job inside a state answer. URL is
// empty where the API sends none.
type KindCount struct {
	Kind     Kind   `json:"kind"`
	Label    string `json:"label"`
	OpenJobs int    `json:"open_jobs"`
	URL      string `json:"url"`
}

// StateSummary is one state in the every-state answer.
type StateSummary struct {
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	OpenJobs  int         `json:"open_jobs"`
	Employers int         `json:"employers"`
	TopKinds  []KindCount `json:"top_kinds"`
	URL       string      `json:"url"`
}

// StatePay is the hourly pay quartiles of one kind of job in a state.
type StatePay struct {
	// Kind is set in a state's by-kind list and empty in a state-and-kind answer.
	Kind Kind `json:"kind"`
	// P25 is the 25th percentile of the posted hourly rates.
	P25 float64 `json:"p25"`
	// Median is the median posted hourly rate.
	Median float64 `json:"median"`
	// P75 is the 75th percentile.
	P75 float64 `json:"p75"`
	// JobsWithRate is how many jobs post an hourly rate.
	JobsWithRate int `json:"jobs_with_rate"`
	// Employers is how many employers posted them (state answer only).
	Employers int `json:"employers"`
}

// Town is a town with open jobs.
type Town struct {
	// Key is the town's name.
	Key  string `json:"key"`
	Jobs int    `json:"jobs"`
	// Slug is the town's slug on CareJobs.Now.
	Slug string `json:"slug"`
}

// Hirer is an employer hiring in a state.
type Hirer struct {
	// Key is the employer's slug.
	Key  string `json:"key"`
	Jobs int    `json:"jobs"`
}

// StateJob is one of a state's newest jobs.
type StateJob struct {
	Title    string `json:"title"`
	Employer string `json:"employer"`
	// Where is the place of the job, for example "Dallas, TX".
	Where string `json:"where"`
	// Pay is the posted pay as text, empty when the posting states none.
	Pay    string `json:"pay"`
	Listed string `json:"listed"`
	URL    string `json:"url"`
}

// StateResult is the answer of JobsByState. Its shape follows the StateParams,
// and only the fields of that shape are filled:
//
//   - no State: States
//   - a State: State, Name, OpenJobs, Employers, URL, ByKind, PayByKind, Towns,
//     Hiring, Newest
//   - a State and a Kind: State, Kind, OpenJobs, URL, and when the slice is big
//     enough for a page of its own also Employers, Pay, Towns, Hiring, Newest
type StateResult struct {
	// States is every US state with its open jobs (no State given).
	States []StateSummary `json:"states"`

	// State is the two-letter state code.
	State string `json:"state"`
	// Name is the state's name.
	Name string `json:"name"`
	// Kind is the kind of job, in a state-and-kind answer.
	Kind Kind `json:"kind"`
	// OpenJobs is the open jobs in the state (or of the kind in the state).
	OpenJobs int `json:"open_jobs"`
	// Employers is how many employers have open jobs there.
	Employers int `json:"employers"`
	// URL is the page of the answer on CareJobs.Now.
	URL string `json:"url"`
	// ByKind is the state's open jobs by kind of job.
	ByKind []KindCount `json:"by_kind"`
	// PayByKind is the hourly pay quartiles by kind of job in the state.
	PayByKind []StatePay `json:"pay_by_kind"`
	// Pay is the hourly pay quartiles of the kind in the state. Nil when no job
	// posts a rate or the slice is too small.
	Pay *StatePay `json:"pay"`
	// Towns are the towns with the most jobs.
	Towns []Town `json:"towns"`
	// Hiring are the employers with the most jobs.
	Hiring []Hirer `json:"hiring"`
	// Newest are the newest jobs, at most the Limit.
	Newest []StateJob `json:"newest"`

	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Employers and Employer
// ---------------------------------------------------------------------------

// EmployerSummary is one entry of the employer list.
type EmployerSummary struct {
	// Slug is the identifier other calls take, for example "cvs-health".
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Type says whether it is an employer, an AI-training program or a job board.
	Type EmployerType `json:"type"`
	// Model is the business model, for example "Pharmacy and retail health".
	Model string `json:"model"`
	// Care is the label of the kind of care.
	Care string `json:"care"`
	// HQ is the headquarters. Empty when not known.
	HQ string `json:"hq"`
	// States are the two-letter codes of the states it hires in.
	States []string `json:"states"`
	// OpenJobs is how many jobs it has open.
	OpenJobs int `json:"open_jobs"`
	// PostedMedianHourly is the median of the hourly rates its open jobs post.
	// 0 when too few post one.
	PostedMedianHourly float64 `json:"posted_median_hourly"`
	// URL is its page on CareJobs.Now.
	URL string `json:"url"`
}

// EmployerList is the answer of Employers.
type EmployerList struct {
	// Total is how many entries match (all of them, not only those returned).
	Total int `json:"total"`
	// Entries are the entries returned, most open jobs first.
	Entries []EmployerSummary `json:"entries"`
	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// EmployerKind is an employer's open jobs of one kind, with the pay they post.
type EmployerKind struct {
	Kind     Kind   `json:"kind"`
	Label    string `json:"label"`
	OpenJobs int    `json:"open_jobs"`
	// Pay is nil when too few of these jobs post an hourly rate.
	Pay *PayStats `json:"pay"`
}

// Employer is the answer of Client.Employer: one employer, AI-training program
// or job board.
type Employer struct {
	Slug     string       `json:"slug"`
	Name     string       `json:"name"`
	Type     EmployerType `json:"type"`
	Model    string       `json:"model"`
	Care     string       `json:"care"`
	HQ       string       `json:"hq"`
	States   []string     `json:"states"`
	OpenJobs int          `json:"open_jobs"`
	// OpenByKind is its open jobs by kind of job.
	OpenByKind []EmployerKind `json:"open_by_kind"`
	// Pay is the pay its jobs post. Nil when too few post an hourly rate;
	// PayText then says how it posts pay.
	Pay     *PayStats `json:"pay"`
	PayText string    `json:"pay_text"`
	// Newest are its newest open jobs.
	Newest []Job `json:"newest"`
	// BestPaid are its best-paid open jobs.
	BestPaid []Job `json:"best_paid"`
	// URL is its page on CareJobs.Now.
	URL string `json:"url"`
	// JobsURL lists every open job of it on CareJobs.Now. Empty when it has none.
	JobsURL string `json:"jobs_url"`
	// CareersURL is its own careers site. Empty when not known.
	CareersURL string `json:"careers_url"`
	// Text is the server's plain-text summary.
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Tools and raw calls
// ---------------------------------------------------------------------------

// Tool describes one tool as the server's tools/list answer does.
type Tool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// InputSchema is the tool's JSON Schema for its arguments, undecoded.
	InputSchema json.RawMessage `json:"inputSchema"`
	// Annotations are the MCP behaviour hints of the tool.
	Annotations ToolAnnotations `json:"annotations"`
}

// ToolAnnotations are the MCP behaviour hints of a tool. Every CareJobs.Now tool
// is read-only and idempotent.
type ToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

// RawResult is the answer of Client.Call.
type RawResult struct {
	// Text is the server's plain-text summary.
	Text string
	// Data is the answer's structuredContent, undecoded.
	Data json.RawMessage
}
