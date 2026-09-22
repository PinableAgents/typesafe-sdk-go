package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

// All executable documentation uses LOCAL FIXTURES, not model inference.
// In an application use NewClient(typesafe.Config{}) and TYPESAFE_API_KEY instead.
func ExampleClient_SystemOne() {
	server := mockapi.New()
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		panic(err)
	}
	defer client.Close()
	request := typesafe.SystemOneRequest{State: map[string]any{"task": "Explain this code without changing files."}, Questions: typesafe.Questions{
		"route": typesafe.ChoiceLabels("What activity is requested?", "explain", "change", "review"),
		"scope": typesafe.ScoreLevels("How broad is the work?", "One function.", "One component.", "Several components."),
		"write": typesafe.Noul{Instructions: "Does the user request persistent changes?"},
	}}
	response, err := client.SystemOne(context.Background(), request)
	if err != nil {
		panic(err)
	}
	if err := response.ValidateFor(request.Questions); err != nil {
		panic(err)
	}
	fmt.Println(response.Choices["route"].Choice)
	fmt.Println(response.Scores["scope"].Score)
	fmt.Println(response.Nouls["write"].Noul)
	// Output:
	// explain
	// 0
	// 0.02
}

func ExampleClient_ListModels() {
	server := mockapi.New()
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		panic(err)
	}
	defer client.Close()
	response, err := client.Models().List(context.Background())
	if err != nil {
		panic(err)
	}
	for _, model := range response.Models {
		fmt.Println(model.Name)
	}
	// Output: mock-model
}

type billingResponse struct {
	Answers struct {
		Billing *typesafe.NoulAnswer `json:"billing"`
	} `json:"answers"`
}

func (r *billingResponse) Validate() error {
	if r.Answers.Billing == nil {
		return errors.New("billing answer required")
	}
	a := r.Answers.Billing
	if a.Type != "noul" || math.IsNaN(a.Noul) || math.IsInf(a.Noul, 0) || a.Noul < 0 || a.Noul > 1 {
		return errors.New("invalid billing probability")
	}
	return nil
}

func ExampleClient_SystemOneInto() {
	server := mockapi.New()
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		panic(err)
	}
	defer client.Close()
	var response billingResponse
	_, err = client.SystemOneInto(context.Background(), typesafe.SystemOneRequest{State: "A duplicate charge", Questions: typesafe.Questions{
		"billing": typesafe.Noul{Instructions: "Is this about billing?"},
	}}, &response)
	if err != nil {
		panic(err)
	}
	fmt.Println(response.Answers.Billing.Noul)
	// Output: 0.02
}

func ExampleClient_SystemOne_concurrent() {
	server := mockapi.New()
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		panic(err)
	}
	defer client.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"first task", "second task"} {
		wg.Add(1)
		go func(state string) {
			defer wg.Done()
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: state, Questions: typesafe.Questions{
				"relevant": typesafe.Noul{Instructions: "Is this relevant?"},
			}})
			results <- err
		}(text)
	}
	wg.Wait()
	close(results)
	passed := 0
	for err := range results {
		if err != nil {
			panic(err)
		}
		passed++
	}
	fmt.Println("completed:", passed)
	// Output: completed: 2
}
