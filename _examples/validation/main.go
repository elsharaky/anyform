// Command validation demonstrates goform's input-validation and hardening
// knobs: the ,required and ,default tag options, omitempty on marshal,
// WithStrictUnmarshal for unknown keys, and WithMaxSliceIndex against
// client-supplied slice indices.
package main

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/elsharaky/goform"
)

type Signup struct {
	Email    string   `form:"email,required"`
	Plan     string   `form:"plan,default:free"`
	Nickname string   `form:"nickname,omitempty"`
	Tags     []string `form:"tags"`
}

type Feed struct {
	Items []string `form:"items"`
}

func main() {
	// Marshal: omitempty drops the empty nickname; required/default only
	// affect unmarshalling.
	body, ct, err := goform.Marshal(Signup{
		Email: "ada@example.com",
		Plan:  "pro",
		Tags:  []string{"beta", "founder"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("marshal: %s (%s)\n", body, ct)

	dec := goform.NewDecoder()

	// Unmarshal: "plan" was not submitted, so its default is applied.
	var got Signup
	submitted := url.Values{"email": {"ada@example.com"}, "tags": {"beta", "founder"}}
	if err := dec.Unmarshal(submitted, &got); err != nil {
		panic(err)
	}
	fmt.Printf("unmarshal: %+v\n", got)

	// A required field that was never submitted is an error you can classify
	// with errors.Is.
	var missing Signup
	err = dec.Unmarshal(url.Values{"plan": {"pro"}}, &missing)
	fmt.Printf("missing required: %v (ErrMissingRequired: %t)\n",
		err, errors.Is(err, goform.ErrMissingRequired))

	// Every decode failure is a *DecodingError carrying the offending key.
	var decodeErr *goform.DecodingError
	if errors.As(err, &decodeErr) {
		fmt.Printf("as *DecodingError: key=%q\n", decodeErr.Key)
	}

	// Strict mode rejects unknown keys; the default mode ignores them.
	strict := goform.NewDecoder(goform.WithStrictUnmarshal(true))
	withJunk := url.Values{"email": {"ada@example.com"}, "referrer": {"https://evil.example"}}
	fmt.Println("strict:", strict.Unmarshal(withJunk, &Signup{}))

	var lax Signup
	fmt.Println("non-strict:", dec.Unmarshal(withJunk, &lax))

	// A tiny body can still ask for a huge slice ("items[5000000]=x").
	// WithMaxSliceIndex bounds the index so the allocation stays small.
	bounded := goform.NewDecoder(goform.WithMaxSliceIndex(10))
	fmt.Println("slice index:", bounded.Unmarshal(url.Values{"items[5000000]": {"x"}}, &Feed{}))

	var small Feed
	if err := bounded.Unmarshal(url.Values{"items[1]": {"x"}}, &small); err != nil {
		panic(err)
	}
	fmt.Printf("slice index within bound: %q\n", small.Items[1])
}
