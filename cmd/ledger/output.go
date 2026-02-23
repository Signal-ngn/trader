package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"
)

// PrintTable renders headers and rows as a table to stdout.
func PrintTable(headers []string, rows [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	hdr := make([]any, len(headers))
	for i, h := range headers {
		hdr[i] = h
	}
	table.Header(hdr...)
	for _, row := range rows {
		r := make([]any, len(row))
		for i, v := range row {
			r[i] = v
		}
		_ = table.Append(r...)
	}
	_ = table.Render()
}

// PrintJSON pretty-prints v as JSON to stdout.
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// fmtFloat formats a float64 with up to 8 significant decimal places,
// trimming trailing zeros.
func fmtFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}

// fmtFloat2 formats a float64 to 2 decimal places.
func fmtFloat2(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// fmtTime formats a time.Time as a short human-readable string.
func fmtTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}
