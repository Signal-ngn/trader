package main

import (
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// LivePrice matches the candle JSON returned by GET /prices/{exchange}/{product}.
type LivePrice struct {
	Exchange    string    `json:"exchange"`
	ProductID   string    `json:"product_id"`
	Granularity string    `json:"granularity"`
	Timestamp   time.Time `json:"timestamp"`
	Open        float64   `json:"open"`
	High        float64   `json:"high"`
	Low         float64   `json:"low"`
	Close       float64   `json:"close"`
	Volume      float64   `json:"volume"`
	LastUpdate  time.Time `json:"last_update"`
}

// Product matches the ingestion server product model.
type Product struct {
	Exchange  string `json:"exchange"`
	ProductID string `json:"product_id"`
	Enabled   bool   `json:"enabled"`
}

// livePriceRow is a rendered row for the price table (nil LivePrice means no data).
type livePriceRow struct {
	exchange  string
	product   string
	livePrice *LivePrice
}

var (
	priceExchange    string
	priceGranularity string
	priceAll         bool
)

var priceCmd = &cobra.Command{
	Use:   "price [product]",
	Short: "Show live price for a product (or all products with --all)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		useJSON, _ := cmd.Flags().GetBool("json")
		c := newPlatformClient()

		if priceAll {
			return runPriceAll(c, useJSON)
		}

		if len(args) == 0 {
			return fmt.Errorf("product argument required (or use --all)")
		}
		return runPriceSingle(c, priceExchange, args[0], priceGranularity, useJSON)
	},
}

// runPriceSingle fetches and displays the price for one product.
func runPriceSingle(c *PlatformClient, exchange, product, granularity string, useJSON bool) error {
	q := url.Values{}
	q.Set("granularity", granularity)
	path := fmt.Sprintf("/prices/%s/%s", exchange, product)

	if useJSON {
		_, raw, err := c.GetRaw(c.apiURL(path, q))
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}

	var lp LivePrice
	if err := c.Get(c.apiURL(path, q), &lp); err != nil {
		return err
	}
	printPriceTable([]livePriceRow{{exchange: lp.Exchange, product: lp.ProductID, livePrice: &lp}})
	return nil
}

// runPriceAll fetches prices for all enabled products concurrently.
func runPriceAll(c *PlatformClient, useJSON bool) error {
	q := url.Values{}
	q.Set("enabled", "true")
	var products []Product
	if err := c.Get(c.apiURL("/ingestion/products", q), &products); err != nil {
		return fmt.Errorf("list products: %w", err)
	}

	if len(products) == 0 {
		fmt.Println("No enabled products found.")
		return nil
	}

	type result struct {
		exchange  string
		product   string
		livePrice *LivePrice
	}

	sem := make(chan struct{}, 10)
	results := make([]result, len(products))
	var wg sync.WaitGroup

	for i, p := range products {
		wg.Add(1)
		go func(idx int, exchange, product string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			q := url.Values{}
			q.Set("granularity", priceGranularity)
			path := fmt.Sprintf("/prices/%s/%s", exchange, product)

			var lp LivePrice
			err := c.Get(c.apiURL(path, q), &lp)
			if err != nil {
				results[idx] = result{exchange: exchange, product: product, livePrice: nil}
				return
			}
			results[idx] = result{exchange: exchange, product: product, livePrice: &lp}
		}(i, p.Exchange, p.ProductID)
	}
	wg.Wait()

	// Sort by exchange then product.
	sort.Slice(results, func(i, j int) bool {
		if results[i].exchange != results[j].exchange {
			return results[i].exchange < results[j].exchange
		}
		return results[i].product < results[j].product
	})

	if useJSON {
		var out []LivePrice
		for _, r := range results {
			if r.livePrice != nil {
				out = append(out, *r.livePrice)
			}
		}
		if out == nil {
			out = []LivePrice{}
		}
		return PrintJSON(out)
	}

	rows := make([]livePriceRow, len(results))
	for i, r := range results {
		rows[i] = livePriceRow{exchange: r.exchange, product: r.product, livePrice: r.livePrice}
	}
	printPriceTable(rows)
	return nil
}

// printPriceTable renders a table of live prices.
func printPriceTable(rows []livePriceRow) {
	tableRows := make([][]string, len(rows))
	for i, row := range rows {
		if row.livePrice == nil {
			tableRows[i] = []string{
				row.exchange, row.product,
				"—", "—", "—", "—", "—",
				"no data",
			}
		} else {
			lp := row.livePrice
			tableRows[i] = []string{
				lp.Exchange,
				lp.ProductID,
				fmtFloat(lp.Close),
				fmtFloat(lp.Open),
				fmtFloat(lp.High),
				fmtFloat(lp.Low),
				fmtFloat(lp.Volume),
				fmtAge(lp.LastUpdate),
			}
		}
	}
	PrintTable([]string{"EXCHANGE", "PRODUCT", "PRICE", "OPEN", "HIGH", "LOW", "VOLUME", "AGE"}, tableRows)
}

// fmtAge formats the duration since t as a human-readable age string.
// Ages over 1 hour are prefixed with "!" to signal staleness.
func fmtAge(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}

	var s string
	switch {
	case d < time.Second:
		s = "< 1s"
	case d < time.Minute:
		s = fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		sec := int(d.Seconds()) - m*60
		s = fmt.Sprintf("%dm %ds", m, sec)
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		s = fmt.Sprintf("!%dh %dm", h, m)
	}
	return s
}

func init() {
	priceCmd.Flags().StringVar(&priceExchange, "exchange", "coinbase", "Exchange name")
	priceCmd.Flags().StringVar(&priceGranularity, "granularity", "ONE_MINUTE", "Granularity (ONE_MINUTE, ONE_HOUR, etc.)")
	priceCmd.Flags().BoolVar(&priceAll, "all", false, "Show live prices for all enabled products")
	rootCmd.AddCommand(priceCmd)
}
