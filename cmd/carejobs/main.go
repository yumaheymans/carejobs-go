// Command carejobs searches US healthcare jobs and the pay employers post from
// the command line. It is a thin shell over the carejobs package: free, no API
// key, standard library only.
//
//	carejobs search "icu nurse" --state TX --limit 5
//	carejobs pay --kind nurses
//	carejobs market
//	carejobs state TX
//	carejobs employers --query kaiser
//	carejobs employer kaiser-permanente
//	carejobs tools
//
// Run carejobs --help for every option. The exit code is 0 on success, 1 when
// the API answers with an error (or cannot be reached), and 2 for bad usage.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	carejobs "github.com/yumaheymans/carejobs-go"
)

// version is the version the command prints. It follows the module version, so
// a release of the module is a release of the command.
const version = carejobs.Version

// defaultTimeout is how long a call may take. A first query the server has not
// seen can take up to about 25 seconds while its database wakes up.
const defaultTimeout = 60 * time.Second

// maxTimeoutSeconds caps --timeout so that the conversion to a time.Duration
// cannot overflow.
const maxTimeoutSeconds = 1e6

// Names of the server's tools, as tools/list reports them.
const (
	toolSearch    = "search_healthcare_jobs"
	toolPay       = "get_healthcare_pay"
	toolMarket    = "get_healthcare_job_market"
	toolState     = "get_healthcare_jobs_by_state"
	toolEmployers = "list_healthcare_employers"
	toolEmployer  = "get_healthcare_employer"
)

// kindAliases are the short names people type for a kind. The API itself only
// knows the exact kinds (carejobs.Kinds). The same table is in the npm and
// Python command line tools.
var kindAliases = map[string]carejobs.Kind{
	"registered-nurse":    carejobs.KindNurses,
	"registered-nurses":   carejobs.KindNurses,
	"nurse":               carejobs.KindNurses,
	"rn":                  carejobs.KindNurses,
	"lpn":                 carejobs.KindNurses,
	"lvn":                 carejobs.KindNurses,
	"np":                  carejobs.KindNursePractitioners,
	"nps":                 carejobs.KindNursePractitioners,
	"pa":                  carejobs.KindNursePractitioners,
	"pas":                 carejobs.KindNursePractitioners,
	"nurse-practitioner":  carejobs.KindNursePractitioners,
	"physician-assistant": carejobs.KindNursePractitioners,
	"doctor":              carejobs.KindPhysicians,
	"doctors":             carejobs.KindPhysicians,
	"physician":           carejobs.KindPhysicians,
	"cna":                 carejobs.KindNursingAssistants,
	"cnas":                carejobs.KindNursingAssistants,
	"pct":                 carejobs.KindNursingAssistants,
	"nursing-assistant":   carejobs.KindNursingAssistants,
	"caregiver":           carejobs.KindCaregivers,
	"home-health-aide":    carejobs.KindCaregivers,
	"therapy":             carejobs.KindTherapyRehab,
	"therapist":           carejobs.KindTherapyRehab,
	"mental-health-care":  carejobs.KindMentalHealth,
	"pharmacist":          carejobs.KindPharmacy,
	"surgical-tech":       carejobs.KindSurgicalTechs,
	"surgical":            carejobs.KindSurgicalTechs,
	"ems":                 carejobs.KindEMTsParamedics,
	"emt":                 carejobs.KindEMTsParamedics,
	"paramedic":           carejobs.KindEMTsParamedics,
	"medical-assistant":   carejobs.KindMedicalAssistants,
	"ma":                  carejobs.KindMedicalAssistants,
}

var (
	sorts = []carejobs.Sort{carejobs.SortMixed, carejobs.SortNewest, carejobs.SortHighestPay}

	employerTypes = []carejobs.EmployerType{
		carejobs.EmployerTypeEmployer, carejobs.EmployerTypeAIProgram, carejobs.EmployerTypeJobBoard,
	}

	cares = []carejobs.Care{
		carejobs.CareHealthSystem, carejobs.CareChildrensHospital, carejobs.CareOutpatient,
		carejobs.CareSpecialty, carejobs.CareBehavioralHealth, carejobs.CareAutism,
		carejobs.CareHomeCare, carejobs.CareSeniorCare, carejobs.CareRehab, carejobs.CareDental,
		carejobs.CarePharmacyRetail, carejobs.CareKidney, carejobs.CareLabsImaging,
		carejobs.CareTelehealth, carejobs.CareStaffing, carejobs.CareAITraining,
		carejobs.CareJobBoards,
	}
)

// join renders a list of string-like values as "a, b, c".
func join[T ~string](values []T) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = string(v)
	}
	return strings.Join(parts, ", ")
}

func helpText() string {
	return `carejobs ` + version + `: search US healthcare jobs and the pay employers post (https://carejobs.now)

Usage:
  carejobs search [words] [options]   Search open jobs
  carejobs pay [options]              Posted hourly pay
  carejobs market                     How many jobs are open, by kind, state and employer
  carejobs state [STATE] [options]    Open jobs in one state (no state: every state)
  carejobs employers [words] [opts]   List employers, most open jobs first
  carejobs employer SLUG              One employer
  carejobs tools                      The tools the API offers

Options by command:
  search     --kind K  --employer E  --state S  --remote  --min-hourly USD
             --sort mixed|newest|highest_pay  --limit N (1 to 25)
  pay        --kind K  --employer E
  state      --kind K  --limit N (1 to 25)
  employers  --query Q  --type employer|ai-program|job-board  --care C
             --state S  --kind K  --limit N (1 to 100)

Options for every command:
  --json          Print the raw data as JSON instead of a readable summary
  --base-url URL  API host (default ` + carejobs.DefaultBaseURL + `)
  --timeout SEC   Give up after this many seconds (default 60)
  -h, --help      Show this help
  -v, --version   Show the version

Examples:
  carejobs search "icu nurse" --state TX --limit 5
  carejobs pay --kind nurses
  carejobs state TX --kind nursing-assistants
  carejobs employers --query kaiser
  carejobs employer kaiser-permanente --json

Kinds: ` + join(carejobs.Kinds) + `
Common short names work too (rn, cna, np, doctor, ...).
Care types: ` + join(cares) + `
No API key needed. MCP server: ` + carejobs.DefaultBaseURL + `/api/mcp
`
}

// usageError is a mistake in the command line. It is reported with exit code 2
// and never reaches the API.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &usageError{msg: fmt.Sprintf(format, a...)} }

// flagSpec describes one option. Boolean options take no value.
type flagSpec struct {
	boolean bool
}

var flagSpecs = map[string]flagSpec{
	"help":       {boolean: true},
	"version":    {boolean: true},
	"json":       {boolean: true},
	"remote":     {boolean: true},
	"base-url":   {},
	"timeout":    {},
	"kind":       {},
	"employer":   {},
	"state":      {},
	"min-hourly": {},
	"sort":       {},
	"limit":      {},
	"query":      {},
	"type":       {},
	"care":       {},
}

// shortFlags maps the one-letter options to their long names.
var shortFlags = map[string]string{"h": "help", "v": "version"}

// globalFlags are accepted by every command.
var globalFlags = map[string]bool{"json": true, "base-url": true, "timeout": true, "help": true, "version": true}

// commandFlags lists the options each command takes besides the global ones.
var commandFlags = map[string][]string{
	"search":    {"kind", "employer", "state", "remote", "min-hourly", "sort", "limit", "query"},
	"pay":       {"kind", "employer"},
	"market":    nil,
	"state":     {"state", "kind", "limit"},
	"employers": {"query", "type", "care", "state", "kind", "limit"},
	"employer":  nil,
	"tools":     nil,
}

// invocation is a parsed command line: the words that are not options, and the
// options given. Options and words may come in any order, before or after the
// command.
type invocation struct {
	positionals []string
	flags       map[string]string // boolean options hold "true"
	order       []string          // option names in the order given, for error messages
}

func (inv *invocation) has(name string) bool {
	_, ok := inv.flags[name]
	return ok
}

// parseArgs splits argv into words and options. It checks the shape of the
// options (known, with or without a value) but not their values.
func parseArgs(argv []string) (*invocation, error) {
	inv := &invocation{flags: map[string]string{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			inv.positionals = append(inv.positionals, argv[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			inv.positionals = append(inv.positionals, arg)
			continue
		}

		var name, value string
		hasValue := false
		if long, ok := strings.CutPrefix(arg, "--"); ok {
			name, value, hasValue = strings.Cut(long, "=")
		} else {
			full, known := shortFlags[arg[1:]]
			if !known {
				return nil, usagef("unknown option '%s'.", arg)
			}
			name = full
		}
		spec, known := flagSpecs[name]
		if !known {
			return nil, usagef("unknown option '--%s'.", name)
		}
		if spec.boolean {
			if hasValue {
				return nil, usagef("--%s does not take a value.", name)
			}
			value = "true"
		} else if !hasValue {
			if i+1 >= len(argv) || (strings.HasPrefix(argv[i+1], "-") && argv[i+1] != "-") {
				return nil, usagef("--%s needs a value.", name)
			}
			i++
			value = argv[i]
		}
		if _, seen := inv.flags[name]; !seen {
			inv.order = append(inv.order, name)
		}
		inv.flags[name] = value
	}
	return inv, nil
}

// parseKind turns what the user typed into one of the API's kinds.
func parseKind(input string) (carejobs.Kind, error) {
	key := strings.Join(strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return r == '_' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}), "-")
	for _, kind := range carejobs.Kinds {
		if string(kind) == key {
			return kind, nil
		}
	}
	if kind, ok := kindAliases[key]; ok {
		return kind, nil
	}
	return "", usagef("Unknown kind '%s'. Use one of: %s.", input, join(carejobs.Kinds))
}

// parseChoice matches input against choices, ignoring case and surrounding space.
func parseChoice[T ~string](flag, input string, choices []T) (T, error) {
	key := strings.ToLower(strings.TrimSpace(input))
	for _, choice := range choices {
		if string(choice) == key {
			return choice, nil
		}
	}
	var zero T
	return zero, usagef("Unknown --%s '%s'. Use one of: %s.", flag, input, join(choices))
}

// parsePositiveInt reads a whole number of 1 or more.
func parsePositiveInt(flag, input string) (int, error) {
	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || strings.TrimLeft(input, "0123456789") != "" {
		return 0, usagef("--%s needs a whole number of 1 or more, not '%s'.", flag, input)
	}
	return n, nil
}

// parseTimeout reads --timeout, a number of seconds above 0.
func parseTimeout(input string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(input, 64)
	if err != nil || math.IsNaN(seconds) || seconds <= 0 {
		return 0, usagef("--timeout needs a number of seconds above 0, not '%s'.", input)
	}
	return time.Duration(math.Min(seconds, maxTimeoutSeconds) * float64(time.Second)), nil
}

// request is what a command line asks of the API.
type request struct {
	tool     string // the tool to call; empty for the tools command, which lists them
	args     any    // the tool's arguments
	asJSON   bool
	baseURL  string
	timeout  time.Duration
	listTool bool // the tools command
}

// buildRequest checks the options of a command and turns them into a request.
// Every mistake comes back as a *usageError before anything is sent.
func buildRequest(inv *invocation, command string) (*request, error) {
	allowed, ok := commandFlags[command]
	if !ok {
		return nil, usagef("Unknown command '%s'.", command)
	}
	for _, name := range inv.order {
		if globalFlags[name] {
			continue
		}
		takes := false
		for _, a := range allowed {
			if a == name {
				takes = true
			}
		}
		if !takes {
			return nil, usagef("'%s' does not take --%s.", command, name)
		}
	}
	args := inv.positionals[1:]

	req := &request{asJSON: inv.has("json"), baseURL: carejobs.DefaultBaseURL, timeout: defaultTimeout}
	if inv.has("base-url") {
		base := strings.TrimRight(inv.flags["base-url"], "/")
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			return nil, usagef("--base-url needs an address starting with http:// or https://, not '%s'.", inv.flags["base-url"])
		}
		req.baseURL = base
	}
	if inv.has("timeout") {
		timeout, err := parseTimeout(inv.flags["timeout"])
		if err != nil {
			return nil, err
		}
		req.timeout = timeout
	}

	var kind carejobs.Kind
	if inv.has("kind") {
		var err error
		if kind, err = parseKind(inv.flags["kind"]); err != nil {
			return nil, err
		}
	}
	limit := 0
	if inv.has("limit") {
		var err error
		if limit, err = parsePositiveInt("limit", inv.flags["limit"]); err != nil {
			return nil, err
		}
	}

	switch command {
	case "search":
		query := strings.Join(args, " ")
		if query == "" {
			query = inv.flags["query"]
		}
		p := carejobs.SearchParams{
			Query:    query,
			Kind:     kind,
			Employer: inv.flags["employer"],
			State:    inv.flags["state"],
			Remote:   inv.has("remote"),
			Limit:    limit,
		}
		if inv.has("min-hourly") {
			var err error
			if p.MinHourlyUSD, err = parsePositiveInt("min-hourly", inv.flags["min-hourly"]); err != nil {
				return nil, err
			}
		}
		if inv.has("sort") {
			var err error
			if p.Sort, err = parseChoice("sort", inv.flags["sort"], sorts); err != nil {
				return nil, err
			}
		}
		req.tool, req.args = toolSearch, p
	case "pay":
		if len(args) > 0 {
			return nil, usagef("'pay' takes no arguments, only --kind and --employer.")
		}
		req.tool, req.args = toolPay, carejobs.PayParams{Kind: kind, Employer: inv.flags["employer"]}
	case "market":
		if len(args) > 0 {
			return nil, usagef("'market' takes no arguments.")
		}
		req.tool, req.args = toolMarket, struct{}{}
	case "state":
		if len(args) > 1 {
			return nil, usagef("'state' takes one state, e.g. 'carejobs state TX'.")
		}
		state := inv.flags["state"]
		if len(args) == 1 {
			state = args[0]
		}
		req.tool, req.args = toolState, carejobs.StateParams{State: state, Kind: kind, Limit: limit}
	case "employers":
		query := strings.Join(args, " ")
		if query == "" {
			query = inv.flags["query"]
		}
		p := carejobs.EmployerListParams{Query: query, State: inv.flags["state"], Kind: kind, Limit: limit}
		if inv.has("type") {
			var err error
			if p.Type, err = parseChoice("type", inv.flags["type"], employerTypes); err != nil {
				return nil, err
			}
		}
		if inv.has("care") {
			var err error
			if p.Care, err = parseChoice("care", inv.flags["care"], cares); err != nil {
				return nil, err
			}
		}
		req.tool, req.args = toolEmployers, p
	case "employer":
		if len(args) != 1 || args[0] == "" {
			return nil, usagef("'employer' takes one slug, e.g. 'carejobs employer kaiser-permanente'.")
		}
		req.tool, req.args = toolEmployer, map[string]string{"employer": args[0]}
	case "tools":
		if len(args) > 0 {
			return nil, usagef("'tools' takes no arguments.")
		}
		req.listTool = true
	}
	return req, nil
}

// describe words an API error for a person. The package's own Error() text
// names the tool and the error class, which is noise on a command line.
func describe(err error) string {
	var apiErr *carejobs.Error
	if !errors.As(err, &apiErr) {
		return err.Error()
	}
	switch apiErr.Code {
	case carejobs.CodeHTTP:
		return fmt.Sprintf("HTTP %d: %s", apiErr.Status, apiErr.Message)
	case carejobs.CodeRPC:
		return fmt.Sprintf("rpc error %d: %s", apiErr.RPCCode, apiErr.Message)
	case carejobs.CodeTimeout:
		return apiErr.Message + " (the first query of its kind can take up to 25 seconds; try a longer --timeout)"
	default:
		return apiErr.Message
	}
}

// run is the whole command: it returns the exit code and writes nothing but
// the answer to stdout.
func run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	inv, err := parseArgs(argv)
	if err != nil {
		return usageFailure(stderr, err)
	}
	if inv.has("help") || (len(inv.positionals) > 0 && inv.positionals[0] == "help") {
		return write(stdout, stderr, helpText())
	}
	if inv.has("version") {
		return write(stdout, stderr, version+"\n")
	}
	if len(inv.positionals) == 0 {
		fmt.Fprint(stderr, helpText())
		return 2
	}
	req, err := buildRequest(inv, inv.positionals[0])
	if err != nil {
		return usageFailure(stderr, err)
	}

	client := carejobs.NewClient(
		carejobs.WithBaseURL(req.baseURL),
		carejobs.WithUserAgent("carejobs-cli/"+version),
	)
	ctx, cancel := context.WithTimeout(ctx, req.timeout)
	defer cancel()

	var out string
	if req.listTool {
		tools, err := client.Tools(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "carejobs: %s\n", describe(err))
			return 1
		}
		if out, err = formatTools(tools, req.asJSON); err != nil {
			fmt.Fprintf(stderr, "carejobs: %s\n", err)
			return 1
		}
	} else {
		res, err := client.Call(ctx, req.tool, req.args)
		if err != nil {
			fmt.Fprintf(stderr, "carejobs: %s\n", describe(err))
			return 1
		}
		if out, err = formatResult(res, req.asJSON); err != nil {
			fmt.Fprintf(stderr, "carejobs: %s\n", err)
			return 1
		}
	}
	return write(stdout, stderr, out)
}

// usageFailure reports a bad command line and returns exit code 2.
func usageFailure(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "carejobs: %s\nRun 'carejobs --help' for usage.\n", err)
	return 2
}

// write prints text to stdout. A reader that went away (carejobs market | head)
// is not an error.
func write(stdout, stderr io.Writer, text string) int {
	if _, err := io.WriteString(stdout, text); err != nil && !errors.Is(err, syscall.EPIPE) {
		fmt.Fprintf(stderr, "carejobs: %s\n", err)
		return 1
	}
	return 0
}

// formatResult renders a tool's answer: the server's readable summary, or with
// asJSON the answer's data exactly as the API sent it, indented.
func formatResult(res *carejobs.RawResult, asJSON bool) (string, error) {
	if !asJSON {
		return withNewline(res.Text), nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, res.Data, "", "  "); err != nil {
		return "", fmt.Errorf("the answer is not valid JSON: %w", err)
	}
	return withNewline(buf.String()), nil
}

// formatTools renders the tools/list answer: a name and title per tool, or with
// asJSON the tools with their input schemas.
func formatTools(tools []carejobs.Tool, asJSON bool) (string, error) {
	if asJSON {
		data, err := json.MarshalIndent(tools, "", "  ")
		if err != nil {
			return "", err
		}
		return withNewline(string(data)), nil
	}
	width := 0
	for _, t := range tools {
		width = max(width, len(t.Name))
	}
	var b strings.Builder
	for _, t := range tools {
		title := t.Title
		if title == "" {
			title = t.Description
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, t.Name, title)
	}
	return b.String(), nil
}

func withNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
