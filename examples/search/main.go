// Command search prints the best-paid open nursing jobs in a state.
//
//	go run ./examples/search            # Texas
//	go run ./examples/search OH         # Ohio
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	carejobs "github.com/yumaheymans/carejobs-go"
)

func main() {
	state := "TX"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // a first, uncached query can take about 20 s
	defer cancel()

	c := carejobs.NewClient()
	res, err := c.SearchJobs(ctx, carejobs.SearchParams{
		Kind:  carejobs.KindNurses,
		State: state,
		Sort:  carejobs.SortHighestPay,
		Limit: 5,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d open nursing jobs in %s\n", res.Total, state)
	if res.Pay != nil {
		fmt.Printf("Posted pay: median $%.0f/hr, typical $%.0f to $%.0f/hr\n",
			res.Pay.Median, res.Pay.TypicalLow, res.Pay.TypicalHigh)
	}
	for i, job := range res.Jobs {
		pay := job.Pay
		if pay == "" {
			pay = "pay not posted"
		}
		fmt.Printf("%d. %s | %s | %s | %s\n   %s\n", i+1, job.Title, job.Employer, job.Location, pay, job.URL)
	}
}
