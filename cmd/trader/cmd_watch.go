package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// tradesWatchCmd streams live trade events for an account to stdout as JSONL.
var tradesWatchCmd = &cobra.Command{
	Use:   "watch <account-id>",
	Short: "Stream live trade events for an account (JSONL to stdout)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]

		// Setup signal handling for clean exit.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigChan
			cancel()
		}()

		apiKey, _, err := resolveAPIKey()
		if err != nil {
			return err
		}
		ledgerURL := viper.GetString("trader_url")
		streamURL := ledgerURL + "/api/v1/accounts/" + accountID + "/trades/stream"

		fmt.Fprintf(os.Stderr, "Watching trades for account: %s\n", accountID)
		fmt.Fprintf(os.Stderr, "Streaming from: %s\n", streamURL)
		fmt.Fprintf(os.Stderr, "Press Ctrl-C to stop.\n")

		return sseLoop(ctx, streamURL, apiKey)
	},
}

// sseLoop connects to the SSE endpoint and writes events as JSONL to stdout.
// On disconnect it waits 5 seconds and reconnects.
func sseLoop(ctx context.Context, streamURL, apiKey string) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		if err := connectAndStream(ctx, streamURL, apiKey); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "stream disconnected: %v — reconnecting in 5s\n", err)
		} else {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "stream ended — reconnecting in 5s\n")
		}

		// Wait 5 seconds before reconnecting.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}

		fmt.Fprintf(os.Stderr, "reconnecting...\n")
	}
}

// connectAndStream opens one SSE connection and reads events until it closes.
func connectAndStream(ctx context.Context, streamURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimSpace(data)
			if data != "" {
				fmt.Println(data)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func init() {
	tradesCmd.AddCommand(tradesWatchCmd)
}
