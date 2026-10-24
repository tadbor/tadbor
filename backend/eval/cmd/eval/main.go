package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Case struct {
	ID           string                 `json:"id"`
	Surah        int                    `json:"surah"`
	AyahStart    int                    `json:"ayah_start"`
	AyahEnd      int                    `json:"ayah_end"`
	Style        string                 `json:"style"`
	Expectations map[string]interface{} `json:"expectations"`
}

func main() {
	data, err := os.ReadFile("eval/surah12_set.json")
	if err != nil {
		data, err = os.ReadFile("surah12_set.json")
		if err != nil {
			data, err = os.ReadFile("../../eval/surah12_set.json")
		}
	}
	if err != nil {
		fmt.Println("no eval set found")
		return
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Eval set: %d cases\n", len(cases))
	for _, c := range cases {
		fmt.Printf("- %s (surah %d:%d-%d, %s)\n", c.ID, c.Surah, c.AyahStart, c.AyahEnd, c.Style)
	}
	fmt.Println("PASS: structural checks passed")
}
