// Command nested demonstrates handling of deeply nested structures: nested
// structs, slices of structs, maps, and arrays.
package main

import (
	"fmt"
	"net/url"

	"github.com/elsharaky/anyform"
)

type Address struct {
	City string `form:"city"`
	ZIP  string `form:"zip"`
}

type LineItem struct {
	SKU string `form:"sku"`
	Qty int    `form:"qty"`
}

type Order struct {
	ID       int               `form:"id"`
	ShipTo   Address           `form:"ship_to"`
	Items    []LineItem        `form:"items"`
	Metadata map[string]string `form:"meta"`
}

func main() {
	in := Order{
		ID:     100,
		ShipTo: Address{City: "NYC", ZIP: "10001"},
		Items: []LineItem{
			{SKU: "A1", Qty: 2},
			{SKU: "B2", Qty: 1},
		},
		Metadata: map[string]string{"rush": "yes"},
	}

	enc := anyform.NewEncoder()
	vals, err := enc.Marshal(in)
	if err != nil {
		panic(err)
	}
	fmt.Println("Marshalled URL query:")
	fmt.Println(vals.Encode())

	dec := anyform.NewDecoder()
	var out Order
	src := url.Values{
		"id":             {"100"},
		"ship_to.city":   {"LA"},
		"ship_to.zip":    {"90001"},
		"items[0].sku":   {"X"},
		"items[0].qty":   {"5"},
		"items[1].sku":   {"Y"},
		"items[1].qty":   {"3"},
		"meta[priority]": {"high"},
	}
	if err := dec.Unmarshal(src, &out); err != nil {
		panic(err)
	}
	fmt.Printf("Unmarshalled: %+v\n", out)
}
