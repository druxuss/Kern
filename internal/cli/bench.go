package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/spf13/cobra"
)

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Benchmark the unikernel response time and throughput",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		kernfilePath := projectPath + "/Kernfile"
		config, err := kernfile.ParseKernfile(kernfilePath)
		if err != nil {
			return fmt.Errorf("error reading Kernfile: %w", err)
		}

		if len(config.Network.Ports) == 0 {
			return fmt.Errorf("no ports defined in Kernfile — bench requires at least one port")
		}

		port := config.Network.Ports[0]
		url := fmt.Sprintf("http://localhost:%d", port)

		fmt.Printf("Benchmarking %s v%s...\n\n", config.App.Name, config.App.Version)

		fmt.Println("=== Response Time ===")
		responseTime, rps, avgLatency, err := measureMetrics(url)
		if err != nil {
			return fmt.Errorf("error measuring metrics: %w", err)
		}
		fmt.Printf("Response time: %s\n\n", responseTime.Round(time.Millisecond))

		fmt.Println("=== Throughput ===")
		fmt.Printf("Requests/sec: %.0f\n", rps)
		fmt.Printf("Avg latency:  %s\n\n", avgLatency.Round(time.Microsecond))

		return nil
	},
}

func measureMetrics(url string) (time.Duration, float64, time.Duration, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	doRequest := func() error {
		resp, err := client.Get(url)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.Body.Close()
	}
	if err := doRequest(); err != nil {
		return 0, 0, 0, fmt.Errorf("unikernel is not responding at %s: start it with 'kern run -d' first: %w", url, err)
	}

	start := time.Now()
	if err := doRequest(); err != nil {
		return 0, 0, 0, fmt.Errorf("error measuring response time: %w", err)
	}
	responseTime := time.Since(start)

	requests := 0
	totalLatency := time.Duration(0)
	started := time.Now()
	deadline := started.Add(5 * time.Second)
	for time.Now().Before(deadline) {
		start := time.Now()
		if err := doRequest(); err != nil {
			return responseTime, 0, 0, fmt.Errorf("benchmark request failed: %w", err)
		}
		totalLatency += time.Since(start)
		requests++
	}
	elapsed := time.Since(started)
	if requests == 0 {
		return responseTime, 0, 0, fmt.Errorf("no benchmark requests completed")
	}
	return responseTime, float64(requests) / elapsed.Seconds(), totalLatency / time.Duration(requests), nil
}

func init() {
	rootCmd.AddCommand(benchCmd)
}
