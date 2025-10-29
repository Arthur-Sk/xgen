package main

import (
	"fmt"

	"github.com/Arthur-Sk/xgen/out"
	playgroundValidator "github.com/go-playground/validator/v10"
)

func main() {
	time := out.TTime("11111111:1")
	fmt.Printf("%v\n", time)

	err := time.Validate()
	fmt.Printf("validate() err: %v\n", err)

	dateTime := out.TDateTime("11.11.20111 1111:11")
	driver := out.Driver{
		CrewStartTime: &dateTime,
	}

	err = playgroundValidator.New().Struct(driver)
	fmt.Printf("playground err: %v\n", err)
}
